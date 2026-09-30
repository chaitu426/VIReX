from fastapi import FastAPI

app = FastAPI(title="VIReX ML service")


@app.get("/health")
def health():
    return {"status": "ok"}
