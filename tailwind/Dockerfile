FROM node:20.18.1-bookworm-slim

WORKDIR /opt/tailwind

COPY tailwind/package.json tailwind/package-lock.json ./
RUN npm ci --no-audit --no-fund
