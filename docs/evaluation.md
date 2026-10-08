# Evaluating retrieval (`m3m0s eval`)

`m3m0s eval` runs a golden dataset against a running server and prints, per
ranking mode (`vector`, `keyword`, `hybrid`), the mean recall@k, the mean
estimated tokens of the returned context and the mean latency. Use it before
and after changing ranking, embeddings or extraction; it is a manual tool, not
a CI gate ([D30](decisions/D30-memory-evaluation-recall-at-k.md)).

## Dataset format

A JSON file listing queries and the entry ids a good search must surface:

```json
{
  "queries": [
    { "query": "why was CI red on the last merge", "domain": "codigo:myrepo", "expect": ["mem_AbC123"] }
  ]
}
```

`recall@k` of a query is the fraction of its `expect` ids found in the top `k`
results. Build the dataset from your own memories (`m3m0s recent --json` gives
ids and contents) and mix both kinds of query: paraphrases that share few words
with the memory, and exact-term queries (identifiers, file names, numbers). The
file references real memories, so keep it outside the repository.

## Running

```bash
export M3M0S_URL=... M3M0S_API_KEY=...
m3m0s eval golden.json -k 1,3,5 -mode vector,keyword,hybrid
```

The table has one row per mode: `recall@k` for each `-k`, `MRR` (mean
reciprocal rank of the first expected id within the largest `k`; 0 for a miss),
mean injected tokens, mean latency and the number of failed queries.

To compare two configurations (for example embedding dimensions, see
[D36](decisions/D36-runtime-embedding-config.md) phase 4), run the same golden
file against a server on each configuration, over copies of the same data.
`scripts/synthetic-golden.py <copy-of-m3m0s.db> out.json -n 300` builds a larger
objective dataset: each query is a passage from the middle of a real entry and
the entry is the expected id. It is a proxy (it favours distinctive text and
near-duplicates count as misses), good for A/B comparisons on the same data,
not an absolute quality score. Combine it with a hand-made golden set.

Differences of a single query on a small
dataset are noise: grow the dataset before retuning.
