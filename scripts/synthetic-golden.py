#!/usr/bin/env python3
"""Build a larger golden dataset for `m3m0s eval` from a COPY of the database.

Each query is a sentence-sized passage taken from the MIDDLE of a real entry
(not its first words), and the expected id is that entry. This is an
objective, reproducible proxy (fixed seed), not a human relevance judgement:
it favours entries whose text is distinctive, and near-duplicate entries can
make a correct neighbour count as a miss. Use it to compare two configs on
the same data (for example 1536 vs 512 dimensions, D36 phase 4), not as an
absolute quality score.

Usage: synthetic-golden.py <copy-of-m3m0s.db> <out.json> [-n 300] [--seed 7]
The database is opened read-only. The output references real memories, so
keep it outside the repository.
"""
import argparse, json, random, re, sqlite3, sys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("db")
    ap.add_argument("out")
    ap.add_argument("-n", type=int, default=300)
    ap.add_argument("--seed", type=int, default=7)
    ap.add_argument("--min-chars", type=int, default=150, help="only entries at least this long")
    a = ap.parse_args()

    con = sqlite3.connect(f"file:{a.db}?mode=ro", uri=True)
    rows = con.execute(
        "SELECT id, domain, content FROM entries WHERE length(content) >= ? ORDER BY id",
        (a.min_chars,),
    ).fetchall()
    rnd = random.Random(a.seed)
    rnd.shuffle(rows)

    queries = []
    for id_, domain, content in rows:
        sents = [s.strip() for s in re.split(r"(?<=[.!?])\s+|\n+", content) if 30 <= len(s.strip()) <= 240]
        if len(sents) < 2:
            continue
        mid = sents[1 : len(sents) - 1] or sents[1:]  # never the first sentence
        queries.append({"query": rnd.choice(mid), "domain": domain, "expect": [id_]})
        if len(queries) >= a.n:
            break
    if not queries:
        sys.exit("no eligible entries")
    with open(a.out, "w") as f:
        json.dump({"queries": queries}, f, ensure_ascii=False, indent=1)
    print(f"wrote {len(queries)} queries from {len(rows)} eligible entries")


if __name__ == "__main__":
    main()
