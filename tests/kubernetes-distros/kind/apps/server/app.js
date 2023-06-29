const express = require('express');

express()
  .use(express.json())
  .get('/health', async (_, res) => {
    res.status(200).send();
  })
  .post('/api/checkout', async (req, res) => {
    const order = req.body;
    console.log(`Received request: ${JSON.stringify(order)}`);

    if (Math.random() < 0.3) {
      res.status(500).send({
        orderProcessed: false
      });
    } else {
      res.status(200).send({
        orderProcessed: true
      });
    }
  })
  .listen(process.env.SERVER_PORT || 5000);
