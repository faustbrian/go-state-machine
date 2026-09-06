# Documentation

- [Quick start](quickstart.md)
- [Compiler-checked examples](../examples_test.go)
- [Testing helpers](https://pkg.go.dev/github.com/faustbrian/go-state-machine/statemachinetest)
- [API reference](api.md)
- [Architecture and diagrams](architecture.md)
- [Guards and effects](guards-effects.md)
- [Persistence](persistence.md)
- [Definition evolution](evolution.md)
- [Outbox integration](outbox.md)
- [Replay and debugging](replay-debugging.md)
- [Concurrency semantics](concurrency.md)
- [Performance](performance.md)
- [Operations](operations.md)
- [Troubleshooting](troubleshooting.md)
- [Adoption guide](adoption.md)
- [Boundaries](boundaries.md)
- [FAQ](faq.md)
- [Compatibility](../COMPATIBILITY.md)
- [Support](../SUPPORT.md)
- [Private vulnerability reporting](../SECURITY.md)
- [Changelog](../CHANGELOG.md)
- [License](../LICENSE)

The root package has no database, queue, or transport dependency. Import only
the optional packages an application needs.
