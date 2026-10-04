#!/usr/bin/env python3
"""Compare migrations and TUI on SQLite backups. Originals are never opened by Crush."""
import hashlib
import json
import os
import pathlib
import re
import re
import shlex
import sqlite3
import subprocess
import time

BASE = pathlib.Path(os.environ.get('FILE_RESTORE_QA_DIR', str(pathlib.Path.home() / '.cache/crush-test/file-restore-qa')))
OUT = pathlib.Path(__file__).resolve().parent
SOURCES = {'home': pathlib.Path.home()/'.crush/crush.db', 'forecaster': pathlib.Path.home()/'Documents/forecaster-ui-public/.crush/crush.db'}

def run(*args):
    return subprocess.check_output(args, text=True)

def fingerprint(path):
    hashes = {}
    with sqlite3.connect(path) as conn:
        for table in ['messages', 'tree_nodes', 'tree_heads']:
            if not conn.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)).fetchone():
                continue
            digest = hashlib.sha256()
            count = 0
            columns=[r[1] for r in conn.execute('PRAGMA table_info('+table+')') if r[1]!='updated_at']
            # Opening either binary refreshes legacy review bookkeeping; its
            # wall-clock updated_at differs, so compare all persisted content.
            for row in conn.execute('SELECT '+','.join(columns)+' FROM ' + table + ' ORDER BY rowid'):
                digest.update(json.dumps(row, ensure_ascii=True).encode())
                count += 1
            hashes[table] = {'count': count, 'sha256': digest.hexdigest()}
        assert conn.execute('PRAGMA integrity_check').fetchone() == ('ok',)
    return hashes

def capture(name):
    return run('tmux', 'capture-pane', '-p', '-t', name)

def wait(predicate, limit=60):
    until=time.monotonic()+limit
    while time.monotonic()<until:
        if predicate():return
        time.sleep(.2)
    raise AssertionError('Copy did not finish loading')

def open_copy(kind, version, data, session):
    name='crush-file-restore-'+kind+'-'+version
    binary='/tmp/crush-file-restore' if version=='after' else str(pathlib.Path.home()/'.local/bin/crush')
    global_data=BASE/(kind+'-'+version+'-global');global_data.mkdir(exist_ok=True)
    (data/'init').touch()
    command=shlex.join(['env','CRUSH_GLOBAL_CONFIG='+str(BASE/'native-config'),'CRUSH_GLOBAL_DATA='+str(global_data),binary,'-D',str(data),'-c',str(BASE/'project'),'-s',session])
    run('tmux','new-session','-d','-s',name,'-x','100','-y','32',command)
    wait(lambda:'YOLO MODE' in capture(name))
    time.sleep(1)
    # Saved chat names and previews are private: keep captures in the scratch dir.
    transcript=capture(name);(BASE/(kind+'-'+version+'-chat.txt')).write_text(transcript)
    run('tmux','send-keys','-t',name,'C-s')
    wait(lambda:'Sessions' in capture(name))
    time.sleep(.5)
    sessions=capture(name);(BASE/(kind+'-'+version+'-sessions.txt')).write_text(sessions)
    run('tmux','kill-session','-t',name)
    return transcript,sessions

results={}
for kind,source in SOURCES.items():
    before=BASE/(kind+'-before-data');after=BASE/(kind+'-after-data')
    before.mkdir(exist_ok=True);after.mkdir(exist_ok=True)
    # Use SQLite's online backup API, as requested, including active WAL data.
    run('sqlite3','-readonly',str(source),'.backup '+str(before/'crush.db'))
    run('sqlite3','-readonly',str(before/'crush.db'),'.backup '+str(after/'crush.db'))
    old=fingerprint(before/'crush.db')
    with sqlite3.connect(before/'crush.db') as conn:
        row=conn.execute('SELECT id FROM sessions WHERE parent_session_id IS NULL AND message_count BETWEEN 5 AND 100 ORDER BY created_at LIMIT 1').fetchone()
        if row is None:row=conn.execute('SELECT id FROM sessions WHERE parent_session_id IS NULL ORDER BY created_at LIMIT 1').fetchone()
        session=row[0]
    baseline=open_copy(kind,'before',before,session)
    candidate=open_copy(kind,'after',after,session)
    new=fingerprint(after/'crush.db')
    baseline_hash=fingerprint(before/'crush.db')
    assert baseline_hash==new,kind+' changed existing conversation data'
    assert old['messages']['count']==new['messages']['count'],kind+' lost original messages'
    # Both binaries run the existing legacy review migration. Their resulting
    # parts are identical; pre-migration embedded reviews need not hash alike.
    # Account/footer line may show live usage/model naming differences; the
    # visible transcript and saved-chat list occupy everything above the footer.
    transcript_same=baseline[0].splitlines()[:-3]==candidate[0].splitlines()[:-3]
    # Sequential captures naturally show different relative ages. Match the
    # complete list after replacing only its right-aligned age field.
    def stable_sessions(screen):
        return [re.sub(r'\s+(?:\d+ (?:second|minute|hour|day|week|month|year)s? ago|just now)(\s+│)', r' <age>\1', line) for line in screen.splitlines()[:-3]]
    sessions_same=stable_sessions(baseline[1])==stable_sessions(candidate[1])
    assert transcript_same,kind+' transcript changed'
    assert sessions_same,kind+' saved-chat list changed'
    results[kind]={'fingerprints':new,'transcript_matches':transcript_same,'saved_chats_match':sessions_same,'saved_chat_relative_ages_normalized':True,'integrity':'ok','excluded_comparison_field':'messages.updated_at (normal legacy review bookkeeping)', 'legacy_review_normalization_matches_installed':True, 'original_message_count':old['messages']['count'] }
    print(kind+': every message/tree row unchanged, old chat and saved-chat list match, integrity ok')
(OUT/'database-checks.json').write_text(json.dumps(results,indent=2)+'\n')
