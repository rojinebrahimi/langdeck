FROM python:3.12-slim
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY sdk/python /sdk
RUN pip install --no-cache-dir "langgraph>=0.2" /sdk && useradd -u 10001 -m agent
COPY examples/python-agent/agent.py .
USER 10001
CMD ["python", "agent.py"]
