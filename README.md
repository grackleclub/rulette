# rulette
[![Test](https://github.com/grackleclub/rulette/actions/workflows/test.yml/badge.svg)](https://github.com/grackleclub/rulette/actions/workflows/test.yml)
rule stacking game based on [dropout.tv](https://dropout.tv)'s [_Rulette_ (S7E7](https://www.dropout.tv/game-changer/season:7/videos/rulette)

---

## game states
```mermaid
flowchart TD
  subgraph init
    create --> invite
    invite --> player1
    invite --> player2
    invite --> player3
    player1 --> join
    player2 --> join
    player3 --> join
    join --> start
    start --> play
  end
  spin
  subgraph play
    spin --> rule --> next
    spin --> modifier --> next
    spin --> prompt --> next
    spin --> over
    next --> spin
  end
  play ---> accuse
  subgraph accusations
    accuse --> convict --> consequences
 accuse --> absolve --> consequences
  end
  consequences ---> play
```


## routes
```mermaid
flowchart TD
  subgraph pregame
    root --> create
    create --> join
    join --> game
  end
  subgraph data
    game/data/player
    game/data/player
    game/data/table
  end
  subgraph actions
    game/action/start
    game/action/end
  end
```

