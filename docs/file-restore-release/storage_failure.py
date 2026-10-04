#!/usr/bin/env python3
"""Regression: unusable object storage must not prevent startup or tool replies."""
import json
import os
import pathlib
import shlex
import sqlite3
import subprocess
import time

BASE = pathlib.Path(os.environ['FILE_RESTORE_QA_DIR'])
REPO = pathlib.Path(__file__).resolve().parents[2]
NAME = 'crush-file-restore-storage-failure'
DATA = BASE / 'storage-failure-data'

def run(*args):
    return subprocess.check_output(args, text=True)

def screen():
    return run('tmux', 'capture-pane', '-p', '-t', NAME)

def wait(predicate):
    until = time.monotonic() + 60
    while time.monotonic() < until:
        if predicate():
            return
        time.sleep(.2)
    raise AssertionError('Storage failure regression did not reach expected state')

def sql(query):
    with sqlite3.connect(DATA / 'crush.db') as conn:
        return conn.execute(query).fetchall()

DATA.mkdir()
run('sqlite3', '-readonly', str(BASE / 'native-data/crush.db'), '.backup ' + str(DATA / 'crush.db'))
(DATA / 'file-history').write_text('This deliberately blocks the optional object directory.\n')
(DATA / 'init').touch()
with sqlite3.connect(DATA / 'crush.db') as conn:
    conn.execute('INSERT OR IGNORE INTO file_history_objects(digest,size) VALUES(?,1)', ('a' * 64,))
(REPO / 'restore-qa-fixture/native.txt').write_text('fixture before\n')
command = shlex.join(['env', 'CRUSH_GLOBAL_CONFIG=' + str(BASE / 'native-config'),
    'CRUSH_GLOBAL_DATA=' + str(BASE / 'storage-failure-global'), '/tmp/crush-file-restore',
    '-D', str(DATA), '-c', str(BASE / 'project')])
try:
    run('tmux', 'new-session', '-d', '-s', NAME, '-x', '100', '-y', '32', command)
    wait(lambda: 'Tree fixture' in screen())
    run('tmux', 'send-keys', '-t', NAME, '-l', 'Read and write the fixture file now.')
    run('tmux', 'send-keys', '-t', NAME, 'Enter')
    wait(lambda: sql("SELECT count(*) FROM file_history_changes WHERE json_extract(before_state,'$.reason') LIKE 'File history unavailable%'")[0][0] > 0)
    wait(lambda: 'FILE_RESTORE_FIXTURE_REPLY' in screen())
    run('tmux', 'send-keys', '-t', NAME, '-l', '/tree')
    run('tmux', 'send-keys', '-t', NAME, 'Enter')
    wait(lambda: 'Session tree' in screen())
    run('tmux', 'send-keys', '-t', NAME, *(['Up'] * 20), 'Enter')
    wait(lambda: 'Carry a summary' in screen())
    run('tmux', 'send-keys', '-t', NAME, 'Enter')
    wait(lambda: 'Restore files?' in screen())
    capture = screen()
    assert 'File history unavailable' in capture, capture
    (REPO / 'docs/file-restore-release/storage-failure.txt').write_text(capture)
    print('PASS: app starts with broken history storage; tool reply survives; preview explains unavailable versions.')
finally:
    subprocess.run(['tmux', 'kill-session', '-t', NAME], check=False, capture_output=True)
    assert subprocess.run(['tmux', 'has-session', '-t', NAME], capture_output=True).returncode != 0
