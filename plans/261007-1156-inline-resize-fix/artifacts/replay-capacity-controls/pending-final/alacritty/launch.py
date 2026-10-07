import os,sys,fcntl
paths=['/private/tmp/ask-debug-replay-pending-final/alacritty/control.fifo', '/private/tmp/ask-debug-replay-pending-final/alacritty/status.fifo']
fds=[os.open(p,os.O_RDWR) for p in paths]
safe=[fcntl.fcntl(fd,fcntl.F_DUPFD,10) for fd in fds]
for fd in fds:os.close(fd)
for fd,dest in zip(safe,(3,4)):
 os.dup2(fd,dest,inheritable=True)
 os.close(fd)
env=dict(os.environ,T0_CONTROL="1",T0_TERMINAL_REFLOW="native")
os.execve('/private/tmp/askcore-inline-replay-candidate/candidate',['/private/tmp/askcore-inline-replay-candidate/candidate']+sys.argv[1:],env)
