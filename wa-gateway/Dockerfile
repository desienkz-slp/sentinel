FROM node:20-alpine

WORKDIR /app

RUN apk add --no-cache tzdata

ENV NODE_ENV=production
ENV TZ=Asia/Jakarta

COPY package*.json ./
RUN npm ci --omit=dev || npm install --omit=dev

COPY . .

EXPOSE 3001

VOLUME ["/app/data"]

CMD ["node", "app/server.js"]
