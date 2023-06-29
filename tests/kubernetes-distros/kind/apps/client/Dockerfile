FROM node:18

WORKDIR /code

COPY ./app.js /code/app.js
COPY ./package.json /code/package.json

RUN npm i --omit=dev

LABEL distro-version='DEV'

CMD ["npm", "run", "start"]