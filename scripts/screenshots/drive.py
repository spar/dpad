"""Run a command in a pseudo-terminal, send it keys on a schedule and save
everything it writes at named points, so render.js can replay the screen.

usage: drive.py OUT_DIR COLS ROWS SCENARIO.json -- COMMAND [ARGS...]

A scenario is a JSON list of steps:
  {"wait": seconds}                    sleep
  {"keys": "text", "delay": seconds}   type text, one character at a time
  {"snap": "name"}                     save OUT_DIR/name.bin
  {"rec": "prefix", "fps": n}          start saving OUT_DIR/prefix-NNNN.bin n times a second
  {"stop": true}                       stop recording
  {"sh": "command"}                    start a shell command in the background
Each saved file holds all output so far, so it reproduces the screen at that moment.
"""
import fcntl
import json
import os
import pty
import re
import select
import struct
import subprocess
import sys
import termios
import threading
import time

out_dir, cols, rows, scenario_path = sys.argv[1:5]
cmd = sys.argv[sys.argv.index("--") + 1:]
cols, rows = int(cols), int(rows)
with open(scenario_path) as f:
    steps = json.load(f)
os.makedirs(out_dir, exist_ok=True)

pid, fd = pty.fork()
if pid == 0:
    env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor")
    env.pop("NO_COLOR", None)
    os.execvpe(cmd[0], cmd, env)

fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
buf = bytearray()
lock = threading.Lock()

# Answer the terminal queries a real emulator would, with the colors render.js uses.
REPLIES = [
    (re.compile(rb"\x1b\]11;\?(\x07|\x1b\\)"), b"\x1b]11;rgb:1414/1717/2121\x1b\\"),
    (re.compile(rb"\x1b\]10;\?(\x07|\x1b\\)"), b"\x1b]10;rgb:d8d8/dede/e9e9\x1b\\"),
    (re.compile(rb"\x1b\[6n"), b"\x1b[1;1R"),
    (re.compile(rb"\x1b\[c"), b"\x1b[?62;22c"),
]


def reader():
    while True:
        try:
            ready, _, _ = select.select([fd], [], [], 0.1)
            if not ready:
                continue
            data = os.read(fd, 65536)
        except OSError:
            return
        if not data:
            return
        with lock:
            buf.extend(data)
        for query, reply in REPLIES:
            for _ in query.finditer(data):
                os.write(fd, reply)


def save(name):
    with lock:
        data = bytes(buf)
    with open(os.path.join(out_dir, name + ".bin"), "wb") as f:
        f.write(data)


recording = threading.Event()


def recorder(prefix, fps):
    n = 0
    while recording.is_set():
        save("%s-%04d" % (prefix, n))
        n += 1
        time.sleep(1 / fps)


threading.Thread(target=reader, daemon=True).start()
rec_thread = None
for step in steps:
    if "wait" in step:
        time.sleep(step["wait"])
    elif "keys" in step:
        for ch in step["keys"]:
            os.write(fd, ch.encode())
            time.sleep(step.get("delay", 0.05))
    elif "snap" in step:
        time.sleep(0.15)
        save(step["snap"])
    elif "rec" in step:
        recording.set()
        rec_thread = threading.Thread(target=recorder, args=(step["rec"], step.get("fps", 8)))
        rec_thread.start()
    elif "stop" in step:
        recording.clear()
        rec_thread.join()
    elif "sh" in step:
        subprocess.Popen(step["sh"], shell=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

try:
    os.kill(pid, 15)
    os.waitpid(pid, 0)
except (ProcessLookupError, ChildProcessError):
    pass
