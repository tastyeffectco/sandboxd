# Minimal FastAPI starter — reliable first boot, not fancy.
from fastapi import FastAPI

app = FastAPI()


@app.get("/health")
def health():
    return {"status": "ok"}


@app.get("/")
def root():
    return {"message": "FastAPI app running. Edit main.py."}
