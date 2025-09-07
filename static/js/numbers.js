function attachModalHandlers() {
  const closeBtn = document.getElementById('close-modal');
  const saveBtn = document.getElementById('modal-save-btn');
  if (closeBtn) {
    closeBtn.onclick = function() {
      document.getElementById('points-modal').classList.remove('show');
    };
  }
  if (saveBtn) {
    saveBtn.onclick = function(e) {
      e.preventDefault();
      const newPoints = document.getElementById('modal-points-input').value;
      const playerId = document.getElementById('modal-player-id').value;
      fetch(`/${currentGameId}/action/points`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          player_id: Number(playerId),
          points: Number(newPoints),
        }),
      })
      .then(response => {
        const msg = document.getElementById('modal-msg');
        if (response.ok) {
          msg.textContent = 'Points updated!';
          msg.style.display = 'block';
          msg.style.color = 'green'
          setTimeout(() => {
            msg.style.display = 'none';
            document.getElementById('points-modal').classList.remove('show');
          }, 5000);
          } else if (response.status === 403) {
            msg.textContent = 'Failed. Only the host can change points.';
            msg.style.display = 'block';
            msg.style.color = 'red';
          } else if (response.status === 425) {
            msg.textContent = 'Failed. The game has not started yet.';
            msg.style.display = 'block';
            msg.style.color = 'red';
          } else {
            msg.textContent = 'Failed. Try again.';
            msg.style.display = 'block';
            msg.style.color = 'red';
          }
      })
      .catch(() => {
        const msg = document.getElementById('modal-msg');
        msg.textContent = 'Failed. Try again.';
        msg.style.display = 'block'
        msg.style.color = 'red';
      });
    };
  }
}

document.addEventListener('DOMContentLoaded', attachModalHandlers);
document.addEventListener('htmx:afterSwap', attachModalHandlers);

let currentGameId = null;
let currentPlayerId = null;
function openPointsModal (gameId, playerId, playerName) {
  currentGameId = gameId
  currentPlayerId = playerId;
  document.getElementById('modal-points-input').value = 0;
  document.getElementById('player-name').textContent = playerName;
  document.getElementById('modal-player-id').value = playerId;
  document.getElementById('points-modal').classList.add('show');
}