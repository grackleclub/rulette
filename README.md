# rulette
[![Test](https://github.com/grackleclub/rulette/actions/workflows/test.yml/badge.svg)](https://github.com/grackleclub/rulette/actions/workflows/test.yml)

rule stacking game based on [dropout.tv](https://dropout.tv)'s [_Rulette_ (S7E7)](https://www.dropout.tv/game-changer/season:7/videos/rulette)

---

## gameplay

Players spin that wheel _(wheel)_ to acquire and trade behavioral rules to increasingly silly ends. A host (game creator) moderates the madness.

## development

### requirements
- go
- [sqlc](https://sqlc.dev/)

### development
#### update and test
Updates `sqlc` definitions and runs all tests.
```sh
bin/test
```


#### run
Start the backend (with optional 'debug' level logging):
```
DEBUG=1 go run .
```

#### debug
Fetch the json from the terminal:
```
bin/state
```

Or just visit https://localhost:7777/{game_id}/data/state.

#### mock
Sets up a game, then opens Firefox as specified player.
```sh
bin/mock
```

### database

Setup the data access layer:
```sh
bin/sqlc
```
- SQL schema hand-written [`db/schema`](./db/schema)
- SQL queries hand-written in [`db/queries`](./db/queries)
- [sqlc](https://sqlc.dev/)  reads [db/sqlc.yml](./db/sqlc.yaml) to autogenerate type-safe code in [`db/sqlc`](./db/sqlc)
- callable from Go: 
  ```go
  data, err := queries.DataAccessLayerQueryHere(ctx, args)```

### htmx

[htmx](https://htmx.org) is a JavaScript library for lightweight frontends using HTML as the engine of application state[^HATEOAS].

[^HATEOAS]: https://en.wikipedia.org/wiki/HATEOAS

```sh
bin/htmx
```
Vendors the most recent htmx release in `static/htmx` and updates the version in `go.mod`.
