const axios = require('axios');

const { init } = require('@lumigo/opentelemetry');
const { SpanStatusCode, trace } = require('@opentelemetry/api');

if (!process.env.TARGET_URL) {
  throw new Error("The required environment variable 'TARGET_URL' is not set")
}

(async () => {
  const { tracerProvider } = await init;
  const tracer = trace.getTracer(__filename)
  await tracer.startActiveSpan('batch', async (rootSpan) => {
    try {
      const res = await axios.post(`${process.env.TARGET_URL}/api/checkout`, {
        "reference": "Order1234567",
        "line_items": [
          {
            "name": "60,000 mile maintenance",
            "quantity": "1",
            "base_price_money": {
              "amount": 30000,
              "currency": "USD"
            },
            "note": "1st line item note"
          },
          {
            "name": "Tire rotation and balancing",
            "quantity": "1",
            "base_price_money": {
              "amount": 15000,
              "currency": "USD"
            }
          },
          {
            "name": "Wiper fluid replacement",
            "quantity": "1",
            "base_price_money": {
              "amount": 1900,
              "currency": "USD"
            }
          },
          {
            "name": "Oil change",
            "quantity": "1",
            "base_price_money": {
              "amount": 2000,
              "currency": "USD"
            }
          }
        ]
      });
      console.log(`Request succesful: ${res}`);
    } catch (err) {
      rootSpan.recordException(err);
      rootSpan.setStatus({
        code: SpanStatusCode.ERROR,
      });
      throw err;
    } finally {
      try {
        rootSpan.end();
      } finally {
        await tracerProvider.shutdown();
      }
    }
  });
})();
