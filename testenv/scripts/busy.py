"""Holds MB megabytes and keeps a core busy; the container's cpus limit sets
how busy it looks.

usage: busy.py MB
"""
import sys

ballast = b"x" * (int(sys.argv[1]) * 1024 * 1024)
while True:
    pass
