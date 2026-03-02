# diagrams

Basic backend design and state flow.

## game states
```mermaid
flowchart TD
  subgraph pregame
    create --> invite
    invite --> player1
    invite --> player2
    invite --> player3
    player1 --> join
    player2 --> join
    player3 --> join
    join --> cards --> host --> start
  end
  start --> initiative
  subgraph game
    subgraph decider
      points<-->|or|rejection
    end
    spin --> prompt --> decider
    subgraph turn
      prompt
      spin --> rule
      spin --> modifier
    end
    accuse --> decider
    decider --> initiative -->spin
  end
```


## routes
> [!NOTE]
> This is a pre-production outline, see route declaration in [main.go](./main.go).

    ```mermaid
flowchart LR

  subgraph pregame
    root --> create
    create --> join
    join --> frontend
  end

  subgraph data
    status
    player
    table
  end

  subgraph actions
    start
    spin
    ending
  end

  frontend -->|htmx| data
  frontend -->|post| actions
```
