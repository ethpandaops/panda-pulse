FROM alpine:latest

# Install Chromium and its dependencies, required for Hive screenshots.
RUN apk add --no-cache \
    chromium \
    chromium-chromedriver \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont

# Set Chrome binary location.
ENV CHROME_BIN=/usr/bin/chromium-browser

COPY panda-pulse /usr/local/bin/
ENTRYPOINT ["panda-pulse"] 