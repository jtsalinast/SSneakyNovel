#!/bin/zsh
# Doble clic para abrir la app. Ctrl+C (o cerrar esta ventana) la cierra.
cd "$(dirname "$0")" || exit 1
PORT=${PORT:-48090}
lsof -tiTCP:$PORT -sTCP:LISTEN | xargs kill 2>/dev/null
sleep 0.5
(sleep 1.5; open "http://localhost:$PORT") &
exec ./show-me-the-story
