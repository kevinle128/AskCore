import sys,json,uuid
from pathlib import Path
sys.path.insert(0,str(Path.cwd()/'e2e/tui'))
from run import Session
p=Path(__file__).parent
for backend in ['alacritty','ghostty']:
 s=Session('tui-test','askcore-origin-'+uuid.uuid4().hex[:8],backend,p/'origin-probe.py',p,p/backend,'g3')
 try:
  before=s.state()
  s.resize(13,6)
  after=s.wait(lambda state:'😀Z' in state['text'])
  s.capture('resize')
  print(backend,json.dumps({'before':before,'after':after},ensure_ascii=False),flush=True)
 finally:s.close()
