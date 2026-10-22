# D4 — Host: Docker on the desktop first, a small ARM host later

**Status:** Accepted

**Decision:** validate by running in Docker on the desktop for a period (deploy, test and use it before promoting), and only then migrate to a Raspberry Pi 3 deployed via GitOps.

**Why:** that Raspberry Pi 3 has 1 GB of RAM in total, already shared among several other services — it is better not to put new load there before confirming the real footprint and stability in an environment with more headroom.
