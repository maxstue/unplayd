FROM node:24.21.0-bookworm-slim AS build
WORKDIR /app
RUN npm install -g pnpm@12.11.2
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY apps/user-api/package.json apps/user-api/package.json
COPY apps/catalog-api/package.json apps/catalog-api/package.json
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY apps/web apps/web
RUN pnpm exec vp run build:web

FROM node:24.21.0-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production HOST=0.0.0.0 PORT=8444
COPY --from=build --chown=node:node /app/apps/web/.output .output
USER node
EXPOSE 8444
CMD ["node", ".output/server/index.mjs"]
