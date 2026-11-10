"""JSON logger, as emitted by structured-logging libraries.

Exercises: JSON "level" detection, multi-container log merging.
"""
import itertools
import json
import random
import sys
import time

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
