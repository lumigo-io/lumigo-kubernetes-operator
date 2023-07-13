
console.log("started");

const express = require('express')
const app = express()
const port = 3000

app.get('/', (req, res) => {
  res.send('Hello World!!! a')
})

app.get('/throw-error', (req, res) => {
    throw new Error("my error");
})

app.get('/null-error', (req, res) => {
    access.nothing();
})

app.listen(port, () => {
  console.log(`Example app listening on port ${port}`)
})