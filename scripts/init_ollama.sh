#!/bin/bash
# Initialize the default small models in Ollama.
# Run after `docker-compose up -d ollama`.
set -e

echo "Pulling small models for offloading..."

# phi3:mini (~380MB) — good for simple classification and extraction
docker exec codergag-ollama ollama pull phi3:mini

# gemma2:latest (~5GB) — better quality, still small enough for simple tasks
# docker exec codergag-ollama ollama pull gemma2:latest

echo "Done. Small-model offloading is ready."
echo "Update your config.yml:"
echo '  llm:'
echo '    enabled: true'
echo '    provider: ollama'
echo '    endpoint: http://localhost:11434/api/chat'
echo '    model: phi3:mini'
