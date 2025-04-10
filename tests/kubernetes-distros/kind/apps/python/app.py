import sys
import time
import logging
import json

logger = logging.getLogger("test")
logger.setLevel(logging.INFO)

# Non-mandatory in our OTEL setup, but recommended for troubleshooting - adds a console handler to see the logs in the console
console_handler = logging.StreamHandler()
console_handler.setLevel(logging.INFO)
logger.addHandler(console_handler)

while True:
    message = sys.argv[1] if len(sys.argv) > 1 else "Hello, World!"
    formatter = json.dumps if len(sys.argv) > 2 and sys.argv[2] == "json" else str
    logger.info(formatter({"message": message}))
    try:
        from lumigo_opentelemetry import logger_provider
        logger_provider.force_flush()
    except Exception as e:
        logger.error(f"Failed to flush logs via distro: {e}")
    time.sleep(5)