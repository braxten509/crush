#!/usr/bin/env python3
"""Reopen the saved fixture chat and exercise restore/undo without model calls."""
import json
import fcntl
import os
import pathlib
import shlex
import subprocess
import sqlite3
import time

REPO = pathlib.Path(__file__).resolve().parents[2]
BASE = pathlib.Path.home() / '.cache/crush-test/file-restore-review-qa'
NAME = 'crush-file-restore-pin-check'
PATH = REPO / 'restore-qa-fixture/native.txt'

def tmux(*args):
    return subprocess.check_output(['tmux', *args], text=True)

def screen():
    return tmux('capture-pane', '-p', '-t', NAME)

def wait_for(predicate):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.2)
    raise AssertionError('UI did not reach expected state: ' + screen())

def keys(*values):
    tmux('send-keys', '-t', NAME, *values)
    time.sleep(.3)

def send(value):
    tmux('send-keys', '-t', NAME, '-l', value)
    keys('Enter')

def tree():
    print('Opening session tree', flush=True)
    send('/tree')
    wait_for(lambda: 'Session tree' in screen())

# This saved chat and named fixture are shared by review workers.
fixture_lock = (BASE / 'pinned-restore-qa.lock').open('a')
fcntl.flock(fixture_lock, fcntl.LOCK_EX)

config = BASE / 'native-config/crush.json'
settings = json.loads(config.read_text())
assert settings.get('options', {}).get('notifications') == 'disabled'
assert not PATH.exists(), 'Disposable fixture path is already occupied'
PATH.parent.mkdir(exist_ok=True)
PATH.write_text('fixture after\n')
with sqlite3.connect('file:' + str(BASE / 'native-data/crush.db') + '?mode=ro', uri=True) as connection:
    session = connection.execute('SELECT id FROM sessions WHERE message_count > 0 ORDER BY message_count DESC LIMIT 1').fetchone()[0]
started = False
try:
    command = shlex.join(['env', 'CRUSH_GLOBAL_CONFIG=' + str(config.parent),
                         'CRUSH_GLOBAL_DATA=' + str(BASE / 'native-global'),
                         '/tmp/crush-file-restore-final', '-D', str(BASE / 'native-data'),
                         '-c', str(BASE / 'project'), '--session', session])
    tmux('new-session', '-d', '-s', NAME, '-x', '100', '-y', '32', command)
    started = True
    wait_for(lambda: 'esc' in screen().lower() or 'Tree fixture' in screen())
    time.sleep(1)
    # Every tree selection first asks about a summary. Its title remains
    # visible while that question is open, so wait for the question itself.
    tree()
    keys(*(['Down'] * 20))
    keys('Enter')
    wait_for(lambda: 'Carry a summary' in screen())
    keys('Enter')
    wait_for(lambda: 'Restore files?' in screen() or 'Branch switched.' in screen())
    if 'Restore files?' in screen():
        keys('Right', 'Enter')  # Chat only; the fixture already holds tip bytes.
        wait_for(lambda: 'Branch switched.' in screen())
    wait_for(lambda: 'FILE_RESTORE_FIXTURE_REPLY' in screen())
    time.sleep(.5)
    tree()
    keys(*(['Up'] * 20))
    keys('Enter')
    wait_for(lambda: 'Carry a summary' in screen())
    keys('Enter')
    wait_for(lambda: 'Restore files?' in screen())
    keys('Enter')
    wait_for(lambda: PATH.read_text() == 'fixture before\n')
    (REPO / 'docs/file-restore-release/pinned-restored.txt').write_text(screen())
    keys('C-p')
    send('Undo Last File Restore')
    wait_for(lambda: 'Undo last file restore?' in screen())
    keys('Enter')
    wait_for(lambda: PATH.read_text() == 'fixture after\n')
    (REPO / 'docs/file-restore-release/pinned-undo.txt').write_text(screen())
    print('PASS: rebuilt executable reopened saved native chat, restored original bytes, and undo recovered edited bytes.')
finally:
    if started:
        subprocess.run(['tmux', 'kill-session', '-t', NAME], check=True)
    PATH.unlink()
    if not any(PATH.parent.iterdir()):
        PATH.parent.rmdir()
    print('Cleanup: named detached terminal and disposable file removed.')
