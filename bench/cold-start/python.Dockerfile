# Python: same story — one-file app, full CPython image underneath.
FROM python:alpine
COPY hello.py /hello.py
EXPOSE 8093
CMD ["python", "/hello.py"]
