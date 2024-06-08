import sys
import time
import logging

logger = logging.getLogger("test")
logger.setLevel(logging.DEBUG)

while True:
  logger.info(sys.argv[1])
  time.sleep(5)