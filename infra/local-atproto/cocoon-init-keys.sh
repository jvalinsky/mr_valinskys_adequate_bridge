#!/bin/sh
set -e

# Generate rotation key if it doesn't exist
if [ ! -f /keys/rotation.key ]; then
  echo "Generating rotation key..."
  /cocoon create-rotation-key --out /keys/rotation.key
fi

# Generate JWK if it doesn't exist
if [ ! -f /keys/jwk.key ]; then
  echo "Generating private JWK..."
  /cocoon create-private-jwk --out /keys/jwk.key
fi

echo "Cocoon keys initialized."
