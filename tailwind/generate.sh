# Use npx with explicit packages so CSS @import "tailwindcss" resolves in CI.
npm --prefix tailwind install --no-audit --no-fund
npx --prefix tailwind tailwindcss -i ./tailwind/input.css -o ./cmd/careme/static/tailwind.css --minify
