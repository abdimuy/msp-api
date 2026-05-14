FROM node:20-alpine
RUN apk add --no-cache openjdk17-jre-headless bash
RUN npm install -g firebase-tools
WORKDIR /firebase
RUN printf '{\n  "emulators": {\n    "auth": { "port": 9099, "host": "0.0.0.0" },\n    "ui": { "enabled": false }\n  }\n}\n' > firebase.json
EXPOSE 9099
CMD ["firebase", "emulators:start", "--only", "auth", "--project", "demo-msp-test"]
