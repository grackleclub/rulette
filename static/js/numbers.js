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
        document.getElementById('points-modal').classList.remove('show');
      })
      .catch(error => {
        alert('Failed to update points');
      });
    };
  }
}

document.addEventListener('DOMContentLoaded', attachModalHandlers);
document.addEventListener('htmx:afterSwap', attachModalHandlers);

let currentGameId = null;
let currentPlayerId = null;
function openPointsModal (gameId, playerId, currentPoints, playerName) {
  currentGameId = gameId
  currentPlayerId = playerId;
  document.getElementById('modal-points-input').value = currentPoints;
  document.getElementById('player-name').textContent = playerName;
  document.getElementById('modal-player-id').value = playerId;
  document.getElementById('points-modal').classList.add('show');
}