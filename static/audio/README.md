# audio

Drop short WAV files here with these exact names. `sound.js` loads them and
plays each when an event that concerns *you* arrives (sound is a per-browser
on/off toggle, default on). Missing files are simply silent.

| file | plays when |
|---|---|
| `alert.wav` | your turn · you draw/receive a card · a message or warning shows |
| `happy.wav` | you gain points, or your accusation is upheld |
| `sad.wav`   | you lose points, or your accusation is denied |

Recording notes: trim leading silence (so the onset is immediate), leave a
little peak headroom, mono is fine. Played via the Web Audio API, so any sample
rate works.
