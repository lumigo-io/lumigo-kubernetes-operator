FROM python:3.9-slim

WORKDIR /code

COPY ./app.py /code/app.py

LABEL distro-version='DEV'

CMD ["python", "app.py", "something to say to the logs", "json"]