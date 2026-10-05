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

Differences of a single query on a small
dataset are noise: grow the dataset before retuning.
