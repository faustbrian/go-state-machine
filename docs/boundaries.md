# Boundaries

This library answers one question: given a compiled finite-state definition,
current state, event, typed context, and metadata, which transition is selected
and which effects are planned?

It is not:

- a workflow engine: it does not schedule or resume arbitrary multi-step jobs;
- a saga framework: it does not coordinate distributed compensation;
- a rule engine: guards only accept or reject an already selected edge;
- a queue: the outbox relay delegates publication to an explicit publisher;
- a scheduler: there are no timers or delayed transitions in the machine;
- a dependency-injection container: every clock, ID source, store, handler,
  recorder, codec, and publisher is passed explicitly.

Hierarchical states, parallel regions, history pseudo-states, and timed
transitions are intentionally absent. Do not approximate them with hidden
callbacks or magic state names. A future addition would need complete selection,
entry/exit, replay, persistence, diagram, and concurrency semantics.
