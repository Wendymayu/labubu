#!/usr/bin/env python3
"""Create ~/.labubu_pass from hidden input (getpass).

Run:  python scripts/make_pass.py
The password is read with hidden input and written to ~/.labubu_pass
with owner-only permissions. It is never echoed to the terminal or
shell history.
"""
import getpass
import os
import stat

path = os.path.expanduser("~/.labubu_pass")
pw = getpass.getpass("Root password for root@101.37.215.110: ")
if not pw:
    raise SystemExit("empty password, aborting")
with open(path, "w") as f:
    f.write(pw)
    f.flush()
    os.fsync(f.fileno())
os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)  # 0600
print(f"wrote {path} (mode 0600)")
