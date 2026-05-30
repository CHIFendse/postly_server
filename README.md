# postly_server

Backend Postly — набор микросервисов на Go, взаимодействующих через gRPC. Каждый сервис отвечает за свой домен и может деплоиться независимо.

## Сервисы

|   Сервис   | Описание                                     |
| `gateway`  | Точка входа                                  |
| `auth`     | Регистрация, логин, верификация токенов      |
| `chat`     | Управление чатами и группами                 |
| `messaging`| Сообщения: отправка, удаление, история       |
| `friends`  | Заявки в друзья, список друзей               |
| `realtime` | WebSocket-хаб, broadcast через Redis Pub/Sub |
| `call`     | DTLS/SFU для голосовых и видеозвонков        |



## Структура

```
postly_server/
├── proto/           
├── services/
│   ├── gateway/       
│   ├── auth/
│   ├── chat/
│   ├── messaging/
│   ├── friends/
│   ├── realtime/
│   └── call/
├── docker-compose.yml
└── README.md
```
