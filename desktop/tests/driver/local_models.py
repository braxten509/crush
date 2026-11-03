"""Inspect the real local-model menu on the private hidden screen, without a GPU."""
import os, sys, time
sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, shot, wait_for

probe, out = sys.argv[1:3]
wait_for(probe, lambda p: p.get('connection', {}).get('state') == 'connected', 30)

def run(script):
    return probe_eval(probe, 'window.__probeEval = (() => {' + script + '})();')

check(run('const b=document.querySelector(".composer button.model"); b?.click(); return Boolean(b);'), 'model menu opens')
deadline = time.monotonic() + 8
while time.monotonic() < deadline:
    if run('const b=[...document.querySelectorAll(".model-menu .group")].find(b=>b.textContent.includes("Local models")); b?.click(); return Boolean(b);'):
        break
    time.sleep(.1)

deadline = time.monotonic() + 5
while time.monotonic() < deadline:
    if run('return document.querySelector(".local-status")?.textContent === "Model loaded";'):
        break
    time.sleep(.1)

result=run('''
const rows=[...document.querySelectorAll('.local-row')];
const rect=el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height,right:r.right};};
return rows.map(row=>({row:rect(row),name:rect(row.querySelector('.name')),size:rect(row.querySelector('.local-size')),action:rect(row.querySelector('.local-action')),label:row.querySelector('.local-action').textContent, fits: row.querySelector('.local-action').scrollWidth <= row.querySelector('.local-action').clientWidth, sizeText:row.querySelector('.local-size').textContent}));
''') or []
check(len(result)==3,'all three downloaded models are visible')
if len(result)==3:
    check([r['label'] for r in result]==['Unload','Load','Load'],'loaded and unloaded actions reflect the server state')
    check(max(r['action']['x'] for r in result)-min(r['action']['x'] for r in result)<.1,'action columns align with a long model name')
    check(all(r['size']['right']<=r['action']['x'] and r['name']['right']<=r['size']['x'] for r in result),'names and sizes fit before the buttons')
    check(all(abs((r['action']['y']+r['action']['h']/2)-(r['row']['y']+r['row']['h']/2))<.5 for r in result),'actions are vertically centered')
    check(all(r['fits'] for r in result),'Load and Unload fit fully at compact zoom')
shot(out,'local-models')
finish()
