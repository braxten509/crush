#!/usr/bin/env python3
"""Detached terminal QA. Run each phase through crush bg + systemd-run."""
import json
import os
import pathlib
import shlex
import sqlite3
import subprocess
import sys
import time

BASE = pathlib.Path(os.environ.get('FILE_RESTORE_QA_DIR', str(pathlib.Path.home() / '.cache/crush-test/file-restore-qa')))
REPO = pathlib.Path(__file__).resolve().parents[2]
OUT = REPO / 'docs/file-restore-release'
BIN = '/tmp/crush-file-restore'

def tmux(*args):
    return subprocess.check_output(['tmux', *args], text=True)

def screen(name):
    return tmux('capture-pane', '-p', '-t', name)

def wait_for(predicate, limit=90):
    deadline = time.monotonic() + limit
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.2)
    raise AssertionError('Timed out waiting for test state')

def send(name, text):
    tmux('send-keys', '-t', name, '-l', text)
    tmux('send-keys', '-t', name, 'Enter')

def keys(name, *keys):
    tmux('send-keys', '-t', name, *keys)
    time.sleep(0.3)

def capture(name, filename):
    (OUT / filename).write_text(screen(name))

def start(kind, resume=False):
    name = 'crush-file-restore-' + kind
    data = BASE / (kind + '-data')
    data.mkdir(exist_ok=True)
    (data / 'init').touch()
    env = ['env', 'CRUSH_GLOBAL_CONFIG=' + str(BASE / (kind + '-config')), 'CRUSH_GLOBAL_DATA=' + str(BASE / (kind + '-global'))]
    command = shlex.join(env + [BIN, '-D', str(data), '-c', str(BASE / 'project')] + (['--continue'] if resume else []))
    tmux('new-session', '-d', '-s', name, '-x', '100', '-y', '32', command)
    wait_for(lambda: 'esc' in screen(name).lower() or 'What' in screen(name) or 'Haiku' in screen(name) or 'Tree fixture' in screen(name))
    capture(name, kind + '-start.txt')
    return name

def sql(kind, query):
    with sqlite3.connect(BASE / (kind + '-data/crush.db')) as connection:
        return connection.execute(query).fetchall()

def jump_root(name):
    send(name, '/tree')
    wait_for(lambda: 'Session tree' in screen(name))
    # Home is handled by the filter input, so repeated Up selects the root.
    keys(name, *(['Up'] * 20))
    keys(name, 'Enter')
    wait_for(lambda: 'Carry a summary' in screen(name))
    keys(name, 'Enter')
    wait_for(lambda: 'Restore files?' in screen(name))

def undo(name):
    keys(name, 'C-p')
    time.sleep(0.5)
    send(name, 'Undo Last File Restore')
    # Palette labels are also searchable if the slash command is not registered.
    wait_for(lambda: 'Undo last file restore?' in screen(name))
    keys(name, 'Enter')

phase = sys.argv[1]
if phase in ('native-start', 'claude-start'):
    kind = phase.split('-')[0]
    name = start(kind)
    if kind == 'native':
        send(name, 'Write the fixture file now.')
    else:
        path = REPO / 'restore-qa-fixture/claude.txt'
        send(name, f'Use the Edit tool once to replace "claude before" with "claude after" in {path}. Then say done. No other tools.')
    wait_for(lambda: sql(kind, 'select count(*) from file_history_changes')[0][0] > 0, limit=120)
    capture(name, kind + '-edited.txt')
    print(kind + ': tracked file version saved')
elif phase == 'native-retry':
    send('crush-file-restore-native', 'Read the fixture file, then write the edit.')
    wait_for(lambda: sql('native', 'select count(*) from file_history_changes')[0][0] > 0, limit=90)
    capture('crush-file-restore-native', 'native-edited.txt')
    print('native: tracked file version saved')
elif phase in ('native-restore', 'claude-restore'):
    kind = phase.split('-')[0]
    name = 'crush-file-restore-' + kind
    path = REPO / 'restore-qa-fixture' / (kind + '.txt')
    original = ('fixture before\n' if kind == 'native' else 'claude before\n')
    changed = ('fixture after\n' if kind == 'native' else 'claude after\n')
    assert path.read_text() == changed
    jump_root(name)
    capture(name, kind + '-preview.txt')
    keys(name, 'Enter')
    wait_for(lambda: path.read_text() == original)
    capture(name, kind + '-restored.txt')
    undo(name)
    wait_for(lambda: path.read_text() == changed)
    capture(name, kind + '-undo.txt')
    print(kind + ': edited -> preview -> restored original -> undo returned edited file')
elif phase == 'claude-final':
    name='crush-file-restore-claude'
    tmux('kill-session','-t',name)
    start('claude',resume=True)
    path=REPO/'restore-qa-fixture/claude.txt'
    send(name,'/tree');wait_for(lambda:'Session tree' in screen(name))
    keys(name,*(['Down']*20));keys(name,'Enter');keys(name,'Enter')
    wait_for(lambda:'Restore files?' in screen(name));keys(name,'Right','Enter')
    wait_for(lambda:'Restore files?' not in screen(name))
    jump_root(name);capture(name,'claude-preview.txt')
    keys(name,'Enter');wait_for(lambda:path.read_text()=='claude before\n');capture(name,'claude-restored.txt')
    undo(name);wait_for(lambda:path.read_text()=='claude after\n');capture(name,'claude-undo.txt')
    print('claude: final binary reopened saved live edit, restored original and undid restore')
elif phase == 'preview-sizes':
    name='crush-file-restore-claude'
    tmux('kill-session','-t',name);start('claude',resume=True)
    send(name,'/tree');wait_for(lambda:'Session tree' in screen(name))
    keys(name,*(['Down']*20));keys(name,'Enter');keys(name,'Enter')
    wait_for(lambda:'Restore files?' in screen(name));keys(name,'Right','Enter')
    wait_for(lambda:'Restore files?' not in screen(name))
    jump_root(name)
    for width,height in [(80,24),(40,12)]:
        tmux('resize-window','-t',name,'-x',str(width),'-y',str(height));time.sleep(.5)
        rendered=screen(name)
        assert all(label in rendered for label in ['Restore files','Chat only','Cancel'])
        capture(name,f'preview-{width}x{height}.txt')
    tmux('resize-window','-t',name,'-x','100','-y','32');time.sleep(.3)
    keys(name,'Enter')
    path=REPO/'restore-qa-fixture/claude.txt'
    wait_for(lambda:path.read_text()=='claude before\n')
    undo(name);wait_for(lambda:path.read_text()=='claude after\n')
    print('final binary: 80x24 and 40x12 previews retain all actions; live edit restore/undo passed')
elif phase == 'native-fork-clone':
    name='crush-file-restore-native'
    tmux('kill-session','-t',name)
    start('native',resume=True)
    path=REPO/'restore-qa-fixture/native.txt'
    assert path.read_text()=='fixture after\n'
    send(name,'/tree')
    wait_for(lambda:'Session tree' in screen(name))
    keys(name,*(['Down']*20));keys(name,'Enter');keys(name,'Enter')
    wait_for(lambda:'Restore files?' in screen(name))
    capture(name,'native-conflict-and-chat-only.txt')
    keys(name,'Right','Enter')
    wait_for(lambda:'Restore files?' not in screen(name))
    assert path.read_text()=='fixture after\n'
    send(name,'/fork')
    wait_for(lambda:'Fork ·' in screen(name))
    keys(name,*(['Up']*20));keys(name,'Enter')
    wait_for(lambda:'Restore files?' in screen(name))
    capture(name,'native-fork-preview.txt')
    keys(name,'Escape')
    assert path.read_text()=='fixture after\n'
    # Escape canceled just the restore preview, leaving the prompt picker.
    keys(name,'Enter');wait_for(lambda:'Restore files?' in screen(name))
    keys(name,'Enter')
    wait_for(lambda:path.read_text()=='fixture before\n')
    capture(name,'native-fork-restored.txt')
    undo(name)
    wait_for(lambda:path.read_text()=='fixture after\n')
    capture(name,'native-fork-undo.txt')
    keys(name,'C-p');time.sleep(.5);send(name,'Clone Current Branch')
    wait_for(lambda:len(sql('native','select id from sessions where parent_session_id is null'))==3)
    assert path.read_text()=='fixture after\n'
    capture(name,'native-clone.txt')
    print('native: conflict preview, Chat only, Cancel, fork restore, undo in new chat, clone leaves files unchanged')
elif phase == 'capture':
    print(screen(sys.argv[2]))
else:
    raise ValueError(phase)
