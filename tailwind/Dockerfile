FROM node:20-alpine
WORKDIR /opt/tailwind
COPY tailwind/package.json tailwind/package-lock.json ./
RUN npm ci --no-audit --no-fund
ENV TAILWIND_BIN=/opt/tailwind/node_modules/.bin/tailwindcss
ENV NODE_PATH=/opt/tailwind/node_modules
