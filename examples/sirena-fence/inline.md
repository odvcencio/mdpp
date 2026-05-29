# Payments architecture

A sirena diagram, rendered natively by mdpp:

```sirena
service api
database db
cache redis
api -> db: reads "user records"
api -> redis: writes "sessions"
```

Mermaid still works, untouched:

```mermaid
graph TD; A-->B;
```
