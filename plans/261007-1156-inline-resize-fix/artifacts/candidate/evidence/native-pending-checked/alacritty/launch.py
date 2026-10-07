#!/usr/bin/env python3
import os,sys
c=os.open('/private/tmp/askcore-inline-resize-fix-5rjbma6z/resize-evidence/native-pending-checked/alacritty/control.fifo',os.O_RDWR)
s=os.open('/private/tmp/askcore-inline-resize-fix-5rjbma6z/resize-evidence/native-pending-checked/alacritty/status.fifo',os.O_RDWR)
os.dup2(c,3,inheritable=True)
os.dup2(s,4,inheritable=True)
os.set_inheritable(3,True)
os.set_inheritable(4,True)
env=dict(os.environ,T0_CONTROL="1",T0_TERMINAL_REFLOW="native")
os.execve('/private/tmp/askcore-inline-resize-fix-5rjbma6z/candidate',['/private/tmp/askcore-inline-resize-fix-5rjbma6z/candidate']+sys.argv[1:],env)
