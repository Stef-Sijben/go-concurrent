# go-concurrent
Concurrent and thread safe data structures in Go

The goal of this module is to provide a set of containers and other data
structures that are thread-safe and perform well under concurrent
accesses when possible.

## Data Structures

* List. Implements the interface of container.List to provide a drop-in
  replacement. Operations only lock the nodes they access or modify.

## See Also

* [concurrent-map](https://github.com/orcaman/concurrent-map) for a
  sharded map, allowing concurrent access to different shards or reads
  within the same shard.