#!/usr/bin/env python3
"""Stop only this verification run's named resources."""
import pathlib
import subprocess

names = {'crush-file-restore-native','crush-file-restore-claude',
         'crush-file-restore-home-before','crush-file-restore-home-after',
         'crush-file-restore-forecaster-before','crush-file-restore-forecaster-after'}
result = subprocess.run(['tmux','list-sessions','-F','#{session_name}'],text=True,capture_output=True)
for name in result.stdout.splitlines():
    if name in names:
        subprocess.run(['tmux','kill-session','-t',name],check=True)
unit='crush-file-restore-fixture.service'
subprocess.run(['systemctl','--user','stop',unit],capture_output=True)
state=subprocess.run(['systemctl','--user','is-active',unit],text=True,capture_output=True)
assert state.returncode != 0 and state.stdout.strip() in ('inactive','unknown','failed'),state.stdout
result=subprocess.run(['tmux','list-sessions','-F','#{session_name}'],text=True,capture_output=True)
assert not names.intersection(result.stdout.splitlines())
root=pathlib.Path(__file__).resolve().parents[2]/'restore-qa-fixture'
for name in ('native.txt','claude.txt'):
    (root/name).unlink(missing_ok=True)
root.rmdir()
print('PASS: all named test terminals gone; fixture server inactive; disposable files removed.')
