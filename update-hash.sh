#!/usr/bin/env bash
# Script to automatically update the Go modules hash in flake.nix

set -e

echo "Building to get the correct hash..."

# Try to build and capture the error output
build_output=$(nix build . 2>&1 || true)

# Extract the new hash from the error message
new_hash=$(echo "$build_output" | grep "got:" | sed -n 's/.*got: *//p' | tr -d ' ')

if [ -z "$new_hash" ]; then
    echo "No hash mismatch found - build might have succeeded or different error occurred"
    echo "$build_output"
    exit 1
fi

echo "Found new hash: $new_hash"

# Update the flake.nix file
sed -i "s/vendorHash = \"sha256-.*\";/vendorHash = \"$new_hash\";/" flake.nix

echo "Updated flake.nix with new hash"
echo "Running build again to verify..."

nix build .

echo "✓ Build successful!"
