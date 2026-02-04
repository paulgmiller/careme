FROM node:20-alpine AS deps
WORKDIR /opt/tailwind
COPY tailwind/package.json tailwind/package-lock.json ./
RUN npm ci --no-audit --no-fund

FROM node:20-alpine
WORKDIR /workspace
COPY --from=deps /opt/tailwind/node_modules /opt/tailwind/node_modules
ENV TAILWIND_BIN=/opt/tailwind/node_modules/.bin/tailwindcss
