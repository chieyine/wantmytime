FROM node:26.9.0-alpine3.24 AS web-build
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run check && npm run build

FROM node:26.9.0-alpine3.24 AS web-runtime
WORKDIR /app
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3000
COPY --from=web-build /app/build ./build
# package.json marks the build output as ES modules for Node.
COPY --from=web-build /app/package.json ./package.json
EXPOSE 3000
USER node
CMD ["node", "build"]
