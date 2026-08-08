#!/bin/bash

# Red Token - Development Startup Script
# Starts both Go backend and Vue frontend dev server

# set -e

export http_proxy=
export https_proxy=
export ALL_PROXY=

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$PROJECT_ROOT/frontend"

echo "🚀 Starting Red Token Development Environment"
echo "================================================"

# Check if frontend dependencies are installed
if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
  echo "📦 Installing frontend dependencies..."
  cd "$FRONTEND_DIR"
  npm install
  cd "$PROJECT_ROOT"
fi

# Function to cleanup on exit
cleanup() {
  echo ""
  echo "🛑 Shutting down services..."
  kill $(jobs -p) 2>/dev/null || true
  exit 0
}

trap cleanup SIGINT SIGTERM

# Start Go backend
echo "🔧 Starting Go backend on :4000..."
export RT_LISTEN_ADDR="${RT_LISTEN_ADDR:-:4000}"
export RT_DB_PATH="${RT_DB_PATH:-$ROOT_DIR/red-token.db}"
export RT_LOG_LEVEL="${RT_LOG_LEVEL:-info}"
export GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}"
export GOMODCACHE="${GOMODCACHE:-/root/go/pkg/mod}"
mkdir -p "$GOCACHE"
go -C backend run ./cmd/red-token &
BACKEND_PID=$!

# Wait for backend to be ready
echo "⏳ Waiting for backend to start..."
sleep 3

# Start frontend dev server
echo "🎨 Starting frontend dev server on :6174..."
cd "$FRONTEND_DIR"
npm run dev -- --port 6174 &
FRONTEND_PID=$!

cd "$PROJECT_ROOT"

echo ""
echo "✅ Services running:"
echo "   - Backend:  http://localhost:4000"
echo "   - Frontend: http://localhost:6174"
echo ""
echo "Press Ctrl+C to stop all services"

# Wait for both processes
wait
