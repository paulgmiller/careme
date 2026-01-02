#!/bin/bash
set -e

# Build script for Tailwind CSS
# This script regenerates the CSS file from the Tailwind configuration

echo "Building Tailwind CSS..."

cd tailwind

# Check if node_modules exists, install if not
if [ ! -d "node_modules" ]; then
    echo "Installing dependencies..."
    npm install
fi

# Build the CSS
npm run build:css

cd ..

echo "CSS build complete! Output: static/output.css"
