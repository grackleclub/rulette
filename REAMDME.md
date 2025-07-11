# rulette

Based on DropoutTV's Rulette.

## State Diagram

```mermaid
flowchart TD
  subgraph init
    create --> invite
    invite --> player1
    invite --> player2
    invite --> player3
    player1 --> initiative
    player2 --> initiative
    player3 --> initiative
  end
  initiative --> spin
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
