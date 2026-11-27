"""JSON logger, as emitted by structured-logging libraries.

Exercises: JSON "level" detection, multi-container log merging. With
BALLAST_MB set it also holds that much memory.
"""
import itertools
import json
import os
import random
import sys
import time

ballast = b"x" * (int(os.environ.get("BALLAST_MB", "0")) * 1024 * 1024)

LEVELS = ["info", "info", "debug", "info", "warn", "info", "error"]

for i in itertools.count(1):
    level = LEVELS[i % len(LEVELS)]
    record = {
        "ts": time.time(),
        "level": level,
        "msg": "payment processed" if level != "error" else "payment gateway timeout",
        "order_id": i,
        "amount": round(random.uniform(5, 500), 2),
    }
    print(json.dumps(record), flush=True)
    time.sleep(2)
    if i % 10 == 0:
        print("Traceback (most recent call last): simulated stack trace on stderr", file=sys.stderr, flush=True)
