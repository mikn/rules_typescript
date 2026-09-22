### Breaking

- `ts_compile` and `ts_test` now default to source mode (`emit = False`). Opt into built JavaScript/declarations with `emit = True` for output-consuming boundaries; analysis errors name both consumer and source owner. Gazelle derives emission opt-ins from resolved Node consumers and package output contracts.
