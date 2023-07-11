FROM node:18
WORKDIR /home/node/app
RUN chown -R node:node /home/node/app
USER 1000
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 3000
CMD [ "npm", "start" ]
