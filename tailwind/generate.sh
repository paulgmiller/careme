# Use npx with explicit packages so CSS @import "tailwindcss" resolves in CI.
npx --yes -p tailwindcss@4.1.18 -p @tailwindcss/cli@4.1.18 tailwindcss -i ./tailwind/input.css -o ./static/tailwind.css --minify
