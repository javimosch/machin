# Node: the app is one line, but you ship the whole interpreter image under it.
FROM node:alpine
COPY hello.js /hello.js
EXPOSE 8092
CMD ["node", "/hello.js"]
