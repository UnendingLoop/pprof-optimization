# Pprof-optimization

Для проведения оптимизации был выбран проект Level0 (исходник тут: https://github.com/UnendingLoop/Level0) - он полностью скопирован в текущий проект и все внесенные в него изменения не затрагивают исходник.

В рамках оптимизации было выполнено следующее:
1. подключение эндпоинта профилировщика;
2. подбор нагрузки, достаточной для выявления мест проседания кода в рантайме;
3. первичное(эталонное "до") снятие профилей с: 
   - cpu,
   - alloc,
   - heap,
4. анализ полученных профилей для выявления кандидатов на оптимизацию;
5. выполнение первой оптимизации в коде приложения;
6. снятие контрольных профилей и сравнение с профилями до внедрения оптимизации;
7. итеративное внедрение последующих оптимизаций в код и подведение итогов.

## Используемые команды

Для сбора метрик/снятия профилей использовались следующие команды:
```bash
curl "http://localhost:8081/debug/pprof/heap" > reports/heap_initial
``` 
- снятие профиля кучи после нагрузки;

```bash
curl "http://localhost:8081/debug/pprof/profile?seconds=60" > reports/cpu_initial.pb.gz 
``` 
- снятие профиля нагрузки на ЦП в течение нагрузочного тестирования;

```bash 
curl "http://localhost:8081/debug/pprof/allocs" > reports/allocs_initial.pb.gz 
```
- снятие профиля аллокация памяти (на протяжении всей работы приложения);

```bash 
go tool pprof -top -diff_base=cpu_before.pb.gz cpu_after.pb.gz 
```
- анализ и сравнение профилей "до" и "после"; 

```bash
go tool pprof -top heap_intial.pb.gz
```
- анализ профиля.

## Итерации профилирования/оптимизаций

Условия снятия профилей:

- **Нагрузка:** генерация 15К фейковых заказов и их отправка в Kafka(далее эти заказы валидируются и отправляются в БД/кешируются в памяти).
- **Продолжительность снятия профиля по ЦП:** 60с, в рамках активной фазы нагрузочного теста.
- **Снятие профилей Alloc и Heap** - после окончания нагрузочного теста.

### Стартовое снятие показателей

#### CPU-profile

Преамбула:

```bash
File: orderservice
Build ID: de62ac89963afdcde1015fe768f7da2c9809b190
Type: cpu
Time: 2026-04-05 16:44:04 CEST
Duration: 60.10s, Total samples = 36.02s (59.94%)
Showing nodes accounting for 26.79s, 74.38% of 36.02s total
Dropped 882 nodes (cum <= 0.18s)
```

##### Топ-5 по 'flat' - "чистому" потреблению процессорного времени функцией:

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| internal/runtime/syscall.Syscall6 | 7.89s | 21.90% | 7.89s | 21.90% |
| runtime.nanotime | 2.70s | | 7.50% | | 2.70s | 7.50% |
| runtime.futex | 2.08s | 5.77% | 2.08s | 5.77% |
| time.runtimeNow | 1.45s | 4.03% |1.45s| 4.03% |
| runtime.scanobject | 1.30s | 3.61% | 2.92s | 8.11% |

**Выводы:**
- Значительная доля CPU уходит в системные вызовы (syscall.Syscall6, syscall, futex), что указывает на:
   - интенсивное взаимодействие с сетью (Kafka, БД),
   - частую синхронизацию горутин,
- Высокая доля runtime.nanotime и time.runtimeNow - частые обращения к времени (таймеры, логика, ORM, драйверы)
- runtime.scanobject занимает заметную долю - активная работа GC

В целом CPU нагружен не бизнес-логикой, а инфраструктурными операциями (I/O + runtime).

##### Топ-10 по 'cum' - с учетом выполнения вложенных вызовов:

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| orderservice/internal/kafka.StartConsumer | 0,02s | 0,06% | 17,16s | 47,64% |
|  orderservice/internal/service.(*orderService).AddNewOrder | 0,03s | 0,08% | 16,98s | 47,14% |
| gorm.io/gorm.(*processor).Execute | 0,06s | 0,17% | 12,85s | 35,67% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1 | 0,02s | 0,06% | 12,74s | 35,37% | 
| orderservice/internal/repository.(*orderRepository).AddNewOrder | 0s | 0,00% | 12,74s | 35,37% | 
| gorm.io/gorm.(*DB).FirstOrCreate | 0s | 0,00% | 9,59s | 26,62% | 
| database/sql.withLock | 0,06s | 0,17% | 8,17s | 22,68% | 
| internal/runtime/syscall.Syscall6 | 7,89s | 21,90% | 7,89s | 21,90% | 
| gorm.io/gorm.(*DB).Create | 0s | 0,00% | 7,41s | 20,57% | 
| internal/poll.ignoringEINTRIO | 0s | 0,00% | 7,07s | 19,63% |

**Выводы:**
Основная нагрузка распределяется между ORM-операциями:
   - gorm DB.Create, FirstOrCreate
   - database/sql слои.
Большая часть времени уходит на:
   - выполнение SQL-запросов
   - обработку соединений и блокировок (withLock, queryDC).

Система демонстрирует I/O-bound характер нагрузки:CPU время "растворяется" в ожидании БД и Kafka.

##### Аггрегация по пакетам и рейтинг по 'cum':

| Пакет | flat | flat% | cum |
|-------|------|-------|-----|
| runtime.* | 12,32s | 34,23% | 88,52s |
| orderservice/internal/* | 0,07s | 0,20% | 75,18s |
| gorm.io/gorm.* | 0,89s | 2,48% | 64,19s |
| github.com/jackc/pgx/v5* | 0,41s | 1,14% | 49,59s |
| database/sql.* | 0,31s | 0,87% | 47,72s |
| syscall.* | 0,09s | 0,25% | 27,65s |
| github.com/segmentio/kafka-go.* | 0,07s | 0,20% | 26,72s |
| github.com/go-faker/faker/v4* | 0,72s | 2,00% | 20,63s |
| internal/poll.* | 0,06s | 0,17% | 14,47s |
| net.* | 0,03s | 0,08% | 13,99s |
| internal/runtime/* | 8,29s | 23,01% | 11,09s |
| math/rand.* | 0,82s | 2,28% | 6,36s | 
| github.com/go-playground/validator.* | 0,17s | 0,47% | 4,54s |
| time.* | 1,48s | 4,11% | 4,24s |
| encoding/json.* | 0,09s | 0,25% | 2,61s |
| reflect.* | 0,38s | 1,06% | 1,36s |
| bufio | 0,04s | 0,11% | 1,25s |

**Выводы:**
- runtime.* - крупнейший потребитель CPU:
   - управление памятью
   - GC
   - планировщик горутин
- orderservice/internal/* - минимальная доля flat, но высокая cum - бизнес-логика вызывает цепочки тяжелых операций
- gorm.io/gorm и database/sql - существенная нагрузка: ORM добавляет overhead поверх SQL
- pgx и kafka-go: заметная доля времени на сетевые операции
- faker и math/rand: генерация тестовых данных не бесплатна по CPU - что поделать.
- validator: дополнительная CPU нагрузка на валидацию структур
- reflect и encoding/json присутствуют, но не доминируют

#### Allocations-profile

Преамбула:

```bash
File: orderservice
Build ID: de62ac89963afdcde1015fe768f7da2c9809b190
Type: alloc_space
Time: 2026-04-06 15:06:39 CEST
Showing nodes accounting for 2608.74MB, 90.66% of 2877.36MB total
Dropped 295 nodes (cum <= 14.39MB)
```

##### Топ-15 по 'flat':

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| math/rand.* | 678.72MB | 23,59% | 678.72MB | 23,59% |
| gorm.io/gorm.* | 412,24MB | 14,33% | 468.74MB | 16,29%  |
| strings.genSplit | 87MB | 3,02% | 87MB | 3,02% |
| github.com/go-playground/validator.(*Validate).extractStructCache | 76.01MB | 2,64% | 192.02MB | 6,67% |
| github.com/go-faker/faker/v4/pkg/options.DefaultOption |75.51MB | 2,62% | 75.51MB | 2,62%  |
| gorm.io/gorm/callbacks.ConvertToCreateValues | 73.04MB | 2,54% | 100.04MB | 3,48% |
| github.com/go-playground/validator.New | 69.28MB | 2,41% | 98.28MB | 3,42% |
| gorm.io/gorm.(*DB).Session | 59.01MB | 2,05% | 181.57MB | 6,31% |
| github.com/go-playground/validator.(*Validate).parseFieldTagsRecursive | 56.51MB | 1,96% | 98.51MB | 3,42% |
| database/sql.driverArgsConnLocked | 56.04MB | 1,95% | 56.04MB | 1,95% |
| github.com/go-faker/faker/v4.GetNetworker | 50.51MB | 1,76% | 118.01MB | 4,10% |
| regexp.(*bitState).reset | 49.84MB | 1,73% | 49.84MB | 1,73% |
| gorm.io/gorm.(*Statement).SelectAndOmitColumns | 48.51MB | 1,69% | 48.51MB | 1,69% |
| gorm.io/gorm/clause.Values.Build | 48MB | 1,67% | 83.01MB | 2,89% |
| gorm.io/gorm.(*DB).getInstance | 42.51MB | 1,48% | 207.59MB | 7,21% |

**Выводы:**
- Основной источник аллокаций — генерация данных: math/rand, faker
- ORM (gorm) - второй крупный источник аллокаций: создание структур, построение запросов
- Валидация (validator) активно использует память: кэширование структур, парсинг тегов
- Работа со строками (strings.genSplit) и regex также создаёт заметные аллокации
- Создание объектов конфигурации (validator.New, faker options) повторяется и дорого обходится

##### Топ-15 по 'cum':

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| orderservice/internal/kafka.StartConsumer| 3MB | 0,10% | 1719,33 | 59,75% |
| orderservice/internal/service.(*orderService).AddNewOrder| 22.51MB | 0,78% | 1710,33 | 59,44% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1| 17.01MB | 0,59% | 1176,4 | 40,88% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder| 0 | 0,00% | 1176,4 | 40,88% |
| orderservice/internal/kafka.EmulateMsgSending| 0 | 0,00% | 1055,12 | 36,67% |
| gorm.io/gorm.(*processor).Execute| 3.50MB | 0,12% | 1040,05 | 36,15% |
| orderservice/internal/mocks.GenerateMockOrder| 10MB | 0,35% | 1010,09 | 35,10% |
| gorm.io/gorm.(*DB).FirstOrCreate| 1MB | 0,04% | 960,82 | 33,39% |
| github.com/go-faker/faker/v4.FakeData| 0 | 0,00% | 880,57 | 30,60% |
| github.com/go-faker/faker/v4.getFakedValue| 0 | 0,00% | 815,57 | 28,34% |
| gorm.io/gorm.(*DB).Create| 0 | 0,00% | 788,3 | 27,40% |
| github.com/go-faker/faker/v4.setDataWithTag| 0 | 0,00% | 745,05 | 25,89% |
| github.com/go-faker/faker/v4.userDefinedNumber| 0 | 0,00% | 681,22 | 23,68% |
| math/rand.(*Rand).Perm| 678.72MB | 23,59% | 678,72 | 23,59% |
| github.com/go-faker/faker/v4.RandomInt| 0 | 0,00% | 678,72 | 23,59% |

**Выводы:**
- Основная память потребляется на уровне бизнес-сценария:
   - orderService.AddNewOrder
   - обработка заказов через Kafka consumer
- Большая часть аллокаций происходит в цепочке: генерация - валидация - ORM - БД - Kafka
- faker - ключевой драйвер памяти: генерация полей заказов
- gorm: значительный overhead на создание и обработку моделей
- math/rand (через faker) даёт основной вклад в cumulative allocations
- validator: создаёт кэш и парсит теги, что увеличивает memory footprint
- Репозиторий и сервис: сами по себе недорогие, но инициируют цепочку аллокаций

##### Аггрегация по пакетам и по 'cum':
| Пакет | flat | flat% | cum |
|-------|------|-------|-----|
| orderservice/internal/ | 60,02 | 2,08% | 8785,81 |
| gorm.io/gorm.* | 779,32 | 27,09% | 7023,37 |
| github.com/go-faker/faker/v4.* | 155,02 | 5,39% | 5201,44 |
| github.com/go-playground/validator.* | 246,8 | 8,58% | 1422,03 |
| database/sql.* | 75,54 | 2,63% | 1032,45 |
| math/rand.* | 678,72 | 23,59% | 678,72 |
| github.com/segmentio/kafka-go.* | 64,31 | 2,24% | 650,19 |
| internal/sync.* | 68,01 | 2,36% | 343,04 |
| github.com/jackc/pgx/v5.* | 123,53 | 4,30% | 246,61 |
| Strings.* | 140,02 | 4,86% | 245,52 |
| Regexp.* | 56,84 | 1,98% | 232,88 |
| Sync.* | 39,58 | 0,0138 | 221,79 |
| Reflect.* | 92,03 | 3,20% | 192,06 |
| encoding/json.* | 16,52 | 0,58% | 35,03 |

**Выводы:**
- gorm.io/gorm - один из крупнейших потребителей памяти
- faker и math/rand - главный источник аллокаций при генерации данных
- validator - значительная память уходит на инициализацию и работу с тегами
- orderservice/internal/*: высокая cumulative нагрузка - бизнес-логика агрегирует работу других пакетов
- pgx, database/sql: умеренные аллокации, связанные с подготовкой и выполнением запросов
- kafka-go: стабильный вклад в память при отправке сообщений
- regexp: заметные аллокации при работе с валидацией/парсингом
- reflect: присутствует как косвенный источник аллокаций (через ORM и validator)

#### Heap-profile

Преамбула:

```bash
File: orderservice
Build ID: de62ac89963afdcde1015fe768f7da2c9809b190
Type: inuse_space
Time: 2026-04-06 15:06:24 CEST
Showing nodes accounting for 9256.30kB, 100% of 9256.30kB total
```

Снятие профиля с кучи показывает, что после нагрузочного тестирования и отработки GC приложение занимает ~9MB памяти:

| Функция | flat | flat% | сum |
|---------|------|-------|-----|
| runtime.allocm | 5643.01kB | 60.96% | 60.96% |
| regexp.(*bitState).reset | 528.17kB | 5.71% | 66.67% |
| reflect.growslice | 524.09kB | 5.66% | 72.33% |
| regexp/syntax.(*compiler).inst | 512.69kB | 5.54% | 77.87% |
| orderservice/internal/service.(*orderService).AddNewOrder | 512.23kB | 5.53% | 83.40% |
| github.com/go-playground/validator.(*Validate).parseFieldTagsRecursive | 512.05kB | 5.53% | 88.94% |
| runtime.(*scavengerState).init | 512.05kB | 5.53% | 94.47% |
| github.com/segmentio/kafka-go/protocol.structDecodeFuncOf | 512.02kB | 5.53% | 100,00% |

Это говорит о том, что:

- утечек по памяти у приложения нет;
- есть много аллокаций в куче от runtime.allocm - происходит много созданий новых потоков выполнения, что говорит о большом кол-ве реально блокирующих вызовов(вероятнее всего бд и кафка).

Статистика по GC для следующих сравнений:
- GC runs: 989
- GC pause: 166801750

#### Кандидаты на оптимизацию

| Проблема | Вероятная причина/решение | Комментарии |
| -------- | ------------------------- | ----------- |
| дорогостоящая генерация фейковых данных (math/rand + faker)| сменить генерацию 15К фейковых заказов на чтение готовых моков из файла - это вероятно повлияет и на следующие 3 пунка в таблице | !!! Придется перезамерить эталонные профили после оптимизации чтобы исключить шум от генерации моков !!! |
| работа reflect и json в горячих местах | минимизировать использование reflect в runtime-пути; по возможности заменить на явные структуры/маппинг; уменьшить количество marshal/unmarshal |  |
| активная работа runtime + GC (runtime.* занимает большую долю CPU и памяти) | уменьшить количество аллокаций: переиспользование структур, уменьшение количества временных объектов, батчинг; проверить частоту создания объектов в горячих путях |
| нагрузка на синхронизацию горутин (futex, schedule, locks) | уменьшить конкуренцию: меньше горутин на узких участках, убрать лишние блокировки, пересмотреть архитектуру потоков обработки |
| тяжелые I/O-операции: сеть, БД, Кафка | kafka-writer без bulk-накопления - добавить batching; проверить лимиты соединений к БД (pool, max open/idle conns); уменьшить количество одновременных запросов | Батчинг должен уменьшить кол-во обращений к кафке, но при этом увеличится сам размер сообщения, + это не повлияет на кол-во сообщений в очереди кафки на чтение приложением; для БД как вариант можно еще рассмотреть внедрение воркеров, которые будут обращаться к базе при получении задачи в канал, в который будут писать экземпляры хендлеров - но сомнительно, что это снизит нагрузку по синхронизации потоков в рантайме |
| перегруз в выполнении операций в рамках ORM-обращений к БД | использовать более дешевые ORM-операции; делать INSERT батчами; по возможности перейти на более прямые запросы; отключить лишние callback-и и логирование | это должно уменьшить среднее время выполнения запроса внутри БД. NB отключение/включение логирования orm-запросов уже реализовано через .env - достаточно указать 'GLOBAL_MODE="production"' для отключения |
| частые аллокации в validator (инициализация, парсинг тегов) | создавать validator один раз и переиспользовать; не создавать новый экземпляр на каждый запрос |


### Оптимизация 1 - смена генерации моков на чтение предподготовленных данных из файла

Ссылка на коммит с оптимизацией: https://github.com/UnendingLoop/pprof-optimization/commit/1761f077e196d29ed0f371c9b7fcfd8780fda3fc 

Изначально в проекте было 2 этапа тестов:
1. 10 предподготовленных заказа (5 валидных, 2 дубля и 3 невалидных) из файла;
2. 10 заказов, генерируемых посредством использования "faker" в пакете mocks.

Для снятия профилей была добавлена нагрузка: во втором этапе вместо 10 заказов генерировалось 15К, что привело к повышенному потреблению памяти/аллокаций/процессорного времени и захламлению профилей шумом.

В рамках "Оптимизации 0" было сгенерировано 15К фейковых валидных заказов в файл(включая 84 дубля), который при запуске приложения читается в память в виде слайса, и далее этот слайс читается 5 горутинами параллельно из разных позиций для избежания отправки повторов в Кафку.

### Повторное снятие "эталонных" показателей и сравнение с предыдущими значениями

#### CPU-profile

Преамбула:

```bash
File: orderservice
Build ID: 556c5df50ff62ad34c39489c0203282d1f28020e
Type: cpu
Time: 2026-04-10 20:28:42 CEST
Duration: 60s, Total samples = 31.57s (52.62%)
Showing nodes accounting for 23.96s, 75.89% of 31.57s total
Dropped 770 nodes (cum <= 0.16s)
```

##### Топ-5 по 'flat' - "чистому" потреблению процессорного времени функцией:

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| internal/runtime/syscall.Syscall6 | 9.25s | 29.30% | 9.25s | 29.30% |
| runtime.nanotime | 3.14s | 9.95% | 3.14s | 9.95% |
| runtime.futex | 2.06s | 6.53% | 2.06s | 6.53% |
| time.runtimeNow | 1.69s | 5.35% | 1.69s | 5.35% |
| runtime.scanobject | 0.79s | 2.50% | 1.98s | 6.27% |



**Выводы** - с первых эталонных замеров почти ничего не изменилось:
- Значительная доля CPU уходит в системные вызовы (syscall.Syscall6, syscall, futex), что указывает на:
   - интенсивное взаимодействие с сетью (Kafka, БД),
   - частую синхронизацию горутин,
- Высокая доля runtime.nanotime и time.runtimeNow - частые обращения к времени (таймеры, логика, ORM, драйверы)
- runtime.scanobject занимает заметную долю - активная работа GC

CPU так же нагружен не бизнес-логикой, а инфраструктурными операциями (I/O + runtime).

##### Топ-20 по 'cum' - с учетом выполнения вложенных вызовов:

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| orderservice/internal/kafka.StartConsumer | 0.01s | 0.032% | 18,67s | 59.14% |
| orderservice/internal/service.(*orderService).AddNewOrder | 0s | 0,00% | 18,48s | 58.54% | 
| gorm.io/gorm.(*processor).Execute | 0.12s | 0.38% | 14,54s | 46.06% | 
| orderservice/internal/repository.(*orderRepository).AddNewOrder | 0s | 0,00% | 14,48s | 45.87% | 
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1 | 0s | 0,00% | 14,48s | 45.87% |  
| gorm.io/gorm.(*DB).FirstOrCreate | 0.01s | 0.032% | 11,23s | 35.57% |
| internal/runtime/syscall.Syscall6 | 9.25s | 29.30% | 9,25s | 29.30% |
| database/sql.withLock | 0.04s | 0.13% | 9,11s | 28.86% | 
| gorm.io/gorm.(*DB).Create | 0s | 0,00% | 8,24s | 26.10% | 
| internal/poll.ignoringEINTRIO | 0s | 0,00% | 7,81s | 24.74% | 

**Выводы** - показатели особо не изменились:
- основная нагрузка - ORM-операции;
- львиная доля времени уходит на выполнение SQL-запросов и обработку соединений и блокировок.


##### Аггрегация по пакетам и по 'cum':

| Пакет | flat | flat% | cum |
| ----- | ---- | ----- | --- |
| runtime.* | 10,44s | 33,12% | 79,61s |
| orderservice/internal/* | 0,02s | 0,06% | 70,67s |
| gorm.io/gorm.* | 1,09s | 3,46% | 73,77s |
| github.com/jackc/pgx/v5.* | 0,51s | 1,62% | 57,28s |
| database/sql.* | 0,29s | 0,93% | 53,24s |
| syscall.* | 0,07s | 0,22% | 30,7s |
| internal/poll.* & internal/runtime* | 9,32s | 29,52% | 28,22s |
| github.com/segmentio/kafka-go.* | 0,11s | 0,35% | 27,33s |
| net.* | 0,02s | 0,06% | 15,55s |
| time.* | 1,73s | 5,48% | 5,4s |
| github.com/go-playground/validator.* | 0,15s | 0,47% | 3,79s |
| encoding/json.* | 0,07s | 0,23% | 1,94s |
| bufio | 0,04s | 0,13% | 1,63s |

**Выводы:**
Исчезли из оборота пакеты:
- reflect.*;
- math/rand.*;
- faker;

как и ожидалось после перехода от генерации моков к чтению готовых данных из файла. Остальные потребители примерно сохраняют уровень потребления ресурсов как и раньше.

##### Diff по пакетам и рейтинг по 'cum'

Преамбула:
```bash
File: orderservice
Build ID: 556c5df50ff62ad34c39489c0203282d1f28020e
Type: cpu
Time: 2026-04-05 16:44:04 CEST
Duration: 120.10s, Total samples = 36.02s (29.99%)
Showing nodes accounting for -3.82s, 10.61% of 36.02s total
Dropped 696 nodes (cum <= 0.18s)
```

| Пакет | flat | flat% | cum |
| ----- | ---- | ----- | --- |
| github.com/go-faker/faker/v4.* | -0,79s | 2,19% | -11,15s |
| runtime.* | -2,09s | 12,39% | -8,8s |
| math/rand.* | -0,82s | 2,28% | -6,36s |
| Reflect.* | -0,4s | 1,33% | -1,01s |
| encoding/json.* | -0,19s | 0,53% | -0,74s |
| github.com/go-playground/validator.* | -0,01s | 0,31% | -0,56s |
| github.com/segmentio/kafka-go.* | -0,01s | 0,53% | -0,52s |
| Strings.* | 0s | 0,17% | -0,34s |
| internal/runtime/sync.* | -0,2s | 0,66% | -0,18s |
| Regexp.* | -0,04s | 0,17% | -0,1s |
| Sync.* | 0,06s | 0,28% | -0,05s |
| bufio | -0,03s | 0,08% | -0,02s |
| Time.* | 0,29s | 0,81% | 0,31s |
| internal* | -0,13s | 0,59% | 0,75s |
| Net.* | -0,01s | 0,08% | 1,3s |
| database/sql.* | -0,05s | 0,59% | 1,8s |
| Syscall.* | -0,02s | 0,22% | 3,05s |
| orderservice/internal/* | -0,05s | 0,20% | 4,86s |
| github.com/jackc/pgx/v5.* | 0,07s | 1,65% | 4,94s |
| internal/runtime/* | 0,96s | 5,72% | 0,39s |
| gorm.io/gorm.* | 0,09s | 1,86% | 6,39s |

**Выводы:**
Стало лучше:
1. сильно просел(точнее исчез) faker - снизилось потребление CPU на 11% из-за отказа от генерации данных;
2. как следствие меньше GC и аллокаций:
   - меньше faker - меньше аллокаций;
   - меньше reflect - меньше временных объектов;
   - меньше rand-операций.

Это уменьшит шум при анализе и сравнении следующих итераци профилировки.

Стало хуже:
1. выросли системные вызовы - больше работы с сетью / БД / Kafka, так как система стала ближе к реальной нагрузке;
2. выросло время работы ORM (gorm) - теперь CPU не тратится на faker - стало видно реальную стоимость DB слоя;
3. немного выросли таймеры / время - больше реальной работы - больше системных вызовов времени.

Для всех трех выводов вероятнее всего одна причина: ранее при генерации моков было много невалидных заказов, а теперь почти все 15К фейковых заказов - валидные, что приводит к более частому обращению к БД, что в свою очередь увеличивает кол-во логов от БД - отключение логов запланировано в следующей оптимизации.

#### Allocations-profile

Преамбула:

```bash
File: orderservice
Build ID: 556c5df50ff62ad34c39489c0203282d1f28020e
Type: alloc_space
Time: 2026-04-10 20:31:27 CEST
Showing nodes accounting for 1547.60MB, 88.12% of 1756.29MB total
Dropped 263 nodes (cum <= 8.78MB)
```
##### Топ-15 по 'flat':

| Функция | flat | flat% | cum | cum% |
|---------|------|-------|-----|------|
| gorm.io/gorm.(*Statement).clone | 228,63MB | 13,02% | 252,63MB | 14,38% |
| gorm.io/gorm.(*Statement).AddClause | 172,1MB | 9,80% | 206,61MB | 11,76% |
| github.com/go-playground/validator.New | 71,31MB | 4,06% | 98,81MB | 5,63% |
| github.com/go-playground/validator.(*Validate).extractStructCache | 69MB | 3,93% | 172,51MB | 9,82% |
| gorm.io/gorm/callbacks.ConvertToCreateValues | 66,53MB | 3,79% | 94,04MB | 5,35% |
| github.com/go-playground/validator.(*Validate).parseFieldTagsRecursive | 55,51MB | 3,16% | 93,51MB | 5,32% |
| gorm.io/gorm/clause.Values.Build | 52,5MB | 2,99% | 80,51MB | 4,58% |
| gorm.io/gorm.(*DB).Session | 47,51MB | 2,70% | 156,06MB | 8,89% |
| database/sql.driverArgsConnLocked | 45,52MB | 2,59% | 45,52MB | 2,59% |
| gorm.io/gorm.Scan | 45,51MB | 2,59% | 129,02MB | 7,35% |
| gorm.io/gorm.(*Statement).SelectAndOmitColumns | 43,01MB | 2,45% | 43,01MB | 2,45% |
| github.com/jackc/pgx/v5/stdlib.(*Rows).Next | 40,5MB | 2,31% | 48,5MB | 2,76% |
| strings.genSplit | 39MB | 2,22% | 39MB | 2,22% |
| github.com/jackc/pgx/v5.(*Conn).getRows | 32,51MB | 1,85% | 32,51MB | 1,85% |
| gorm.io/gorm.(*DB).getInstance | 30,01MB | 1,71% | 174,08MB | 9,91% |

**Выводы:**

Как и ожидалось - без генерации моков аллокации уменьшились. Однако, нагрузка по обращениям к БД немного подросла.


##### Топ-15 по 'cum':

| Функция | flat | flat% | cum | cum% |
| ------- |------|-------|-----|------|
| orderservice/internal/kafka.StartConsumer | 2,5MB | 0,14% | 1648,57MB | 93,87% |
| orderservice/internal/service.(*orderService).AddNewOrder | 24,51MB | 1,40% | 1643,07MB | 93,55% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder | 0,5MB | 0,03% | 1136,36MB | 64,70% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1 | 13,5MB | 0,77% | 1135,86MB | 64,67% |
| gorm.io/gorm.(*processor).Execute | 3MB | 0,17% | 1023,41MB | 58,27% |
| gorm.io/gorm.(*DB).FirstOrCreate | 0MB | 0,00% | 949,8MB | 54,08% |
| gorm.io/gorm.(*DB).Create | 0MB | 0,00% | 752,77MB | 42,86% |
| gorm.io/gorm/callbacks.RegisterDefaultCallbacks.SaveAfterAssociations.func4 | 1,5MB | 0,09% | 544,7MB | 31,01% |
| gorm.io/gorm/callbacks.saveAssociations | 1MB | 0,06% | 529,2MB | 30,13% |
| gorm.io/gorm/callbacks.RegisterDefaultCallbacks.Create.func3 | 14MB | 0,80% | 415,61MB | 23,66% |
| gorm.io/gorm.(*Statement).clone | 228,63MB | 13,02% | 252,63MB | 14,38% |
| gorm.io/gorm/callbacks.Query | 0MB | 0,00% | 250,03MB | 14,24% |
| github.com/go-playground/validator.(*Validate).Struct | 0MB | 0,00% | 230,73MB | 13,14% |
| github.com/go-playground/validator.(*Validate).StructCtx | 0MB | 0,00% | 230,73MB | 13,14% |
| gorm.io/gorm.(*DB).Find | 0MB | 0,00% | 214,53MB | 12,21% |

**Выводы:**

На данный момент главные потребители ресурсов - Kafka-консюмер и ORM-операции (и их последующие обращения к БД).

##### Аггрегация по пакетам и по 'cum':
| Пакет | flat | flat% | cum |
| ----- | ---- | ----- | --- |
| gorm.io/gorm.* | 771,8MB | 43,94% | 6993,77MB |
| orderservice/internal/kafka.* | 72,53MB | 4,14% | 5876,18MB |
| github.com/go-playground/validator.* | 230,32MB | 13,12% | 1252,45MB |
| database/sql.* | 75,02MB | 4,27% | 1039,19  MB |
| github.com/segmentio/kafka-go.* | 30,02MB | 1,71% | 329,79MB |
| github.com/jackc/pgx/v5.* | 132,01MB | 7,52% | 286,04MB |
| internal/sync.(*HashTrieMap * | 28MB | 1,59% | 184MB |
| strings.* | 105,02MB | 5,98% | 169,02MB |
| sync.* | 28,56MB | 0,0163 | 149,22MB |
| regexp.* | 29,27MB | 1,67% | 137,13MB |
| reflect | 33,5MB | 1,91% | 72,5MB |
| encoding/json.* | 2MB | 0,11% | 59MB |

**Выводы:**
В разрезе пакетов наблюдается некоторый спад потребления аллокаций, но kafka и gorm все так же в лидерах.

##### Diff по пакетам и рейтинг по 'cum'

Преамбула:
```bash
File: orderservice
Build ID: 556c5df50ff62ad34c39489c0203282d1f28020e
Type: alloc_space
Time: 2026-04-06 15:06:39 CEST
Showing nodes accounting for -1149516.03kB, 39.01% of 2946420.81kB total
Dropped 359 nodes (cum <= 14732.10kB)
```

| Пакет | flat | flat% | cum |
| ----- | ---- | ----- | --- |
| github.com/go-faker/faker/v4.* | -166415,43kB | 5,65% | -5367066,24kB | 
| orderservice/internal/* | 8210,83kB | 1,37% | -3017869,35kB | 
| math/rand.(*Rand).Perm | -695013,44kB | 23,59% | -695013,44kB | 
| github.com/segmentio/kafka-go.* | -31939,53kB | 1,09% | -318453,48kB | 
| github.com/go-playground/validator.* | -14850,2kB | 0,50% | -166004,98kB | 
| internal/sync.* | -35330,55kB | 1,20% | -157196,39kB | 
| gorm.io/gorm.* | -40973,04kB | 3,76% | -137874,8kB | 
| reflect.* | -59924,88kB | 2,03% | -122409,62kB | 
| regexp.* | -25666,46kB | 0,87% | -101826,49kB | 
| strings.* | -42495,89kB | 1,90% | -84991,78kB | 
| database/sql.* | -6671,54kB | 0,51% | -84204,49kB | 
| sync.(*Map)&(*Pool) | -11285,73kB | 0,0038 | -74307,52kB |
| encoding/json.Marshal | -15891,38kB | 0,54% | -19479,66kB | 
| github.com/jackc/pgx/v5/stdlib.* | -8708,78kB | 0,30% | -14376,79kB |

**Выводы:**
Преамбула diff показывает снижение потребления памяти почти на 40% - получается столько ресурса тратилось на генерацию моков.

#### Heap-profile diff

Преамбула: 
```bash
File: orderservice
Build ID: 556c5df50ff62ad34c39489c0203282d1f28020e
Type: inuse_space
Time: 2026-04-06 15:06:24 CEST
Showing nodes accounting for -1.38MB, 15.21% of 9.04MB total
```

| Функция | flat | flat% | сum | cum% |
| ------- | ---- | ----- | --- | ---- |
| runtime.allocm | -2MB | 22,17% | -2MB | 22,17% | 
| runtime/pprof.StartCPUProfile | 1,16MB | 12,79% | 1,16MB | 12,79% | 
| regexp.(*bitState).reset | -0,52MB | 5,71% | -0,52MB | 5,71% | 
| reflect.growslice | -0,51MB | 5,66% | -0,51MB | 5,66% | 
| regexp/syntax.(*compiler).inst | -0,50MB | 5,54% | -0,50MB | 5,54% | 
| github.com/jackc/pgx/v5/pgproto3.NewFrontend | 0,50MB | 5,54% | 0,50MB | 5,54% | 
| orderservice/internal/service.(*orderService).AddNewOrder | -0,50MB | 5,53% | -0,02MB | 0,18% | 
| runtime.malg | 0,50MB | 5,53% | 0,50MB | 5,53% | 
| context.(*cancelCtx).Done | 0,50MB | 5,53% | 0,50MB | 5,53% | 
| runtime.(*scavengerState).init | -0,50MB | 5,53% | -0,50MB | 5,53% | 
| github.com/go-playground/validator.(*Validate).extractStructCache | 0,50MB | 5,53% | 0,50MB | 5,53% | 
| github.com/segmentio/kafka-go/protocol.structDecodeFuncOf | -0,50MB | 5,53% | -0,50MB | 5,53% | 
| github.com/jackc/pgx/v5/pgtype.scanPlanString.Scan | 0,50MB | 5,53% | 0,50MB | 5,53%

**Выводы:**

После отказа от генерации моков профиль кучи показывает, что приложение занимает ~7.8Mb, что на 15% меньше предыдущего значения, хотя такое уменьшение не ожидалось: в предыдущей версии кода после отработки генерации моков, задействованные в ней функции и временные данные должны были очиститься GC, и постоянно используемая приложением память по идее должна была бы быть примерно одинакова в обеих версиях. Предположение: сам бинарник приложения стал чуть легче из-за удаления логики/пакетов генерации моков.

Сравнение общего кол-во циклов запуска GC так же дает смещение в положительную сторону(в следующих итерациях оптимизаций таблица будет дополняться и будет вынесена в отдельный параграф): 

| Версия | GC runs | GC pause, ns |
| ------ | ------- | ------------ |
| Initial result | 989 | 166801750 |
| **Opt. 1 result** | 408 | 62218231 |
 

### Оптимизация 2 - Kafka/DB - инфраструктурные изменения

План изменений:
- сделать bulk-накопление сообщений(500шт) в kafka-writer перед их отправкой;
- сделать bulk-чтение(макс. 10Мб) сообщений из Kafka;
- указать макс. кол-во соединений к БД:
   - MaxOpenConns = 16
   - MaxIdleConns = 8
- отключить логирование GORM(параметр в env.)

Ожидания: 
- уменьшение потребления ЦП (runtime.nanotime, syscall, GC);
- увеличение аллокаций и heap за счет bulk-операций в kafka.

Ссылка на коммит с оптимизацией:

#### Результаты по GC

| Версия | GC runs | GC pause, ns |
| ------ | ------- | ------------ |
| Initial result | 989 | 166801750 |
| Opt. 1 result | 408 | 62218231 |
| **Opt. 2 result** | 146 | 28078875 |

В сравнении с предыдущей оптимизацией, частота запуска GC снизилась почти в 3 раза, как и суммарная длительность STW.

#### Diff CPU

Преамбула:
```bash
File: orderservice
Build ID: 56fd7ab202b9ced3754b0933603c2d482033c547
Type: cpu
Time: 2026-04-10 20:28:42 CEST
Duration: 120s, Total samples = 31.57s (26.31%)
Showing nodes accounting for -1.10s, 3.48% of 31.57s total
Dropped 36 nodes (cum <= 0.16s)
```

Аггрегация по пакетам и рейтинг по 'cum':
| Пакет | flat, s | flat% | сum, s |
| ----- | ------- | ----- | ------ |
| Runtime.* | -1,62 | 16,79% | -10,63 | 
| gorm.io* | 0,03 | 3,87% | -3,11 | 
| segmentio/kafka-go* | 0,04 | 1,15% | -1,24  | 
| Time.* | -0,25 | 0,85% | -1,03 |
| internal/sync.* | -0,03 | 0,35% | -0,47 | 
| Regexp.* | -0,04 | 0,45% | -0,43 | 
| orderservice/internal/* | 0,03 | 0,22% | -0,34 | 
| bufio.(*Reader).Discard | -0,05 | 0,16% | -0,28 | 
| Log.* | -0,03 | 0,10% | -0,22 | 
| Context.* | -0,04 | 0,32% | -0,18 | 
| Fmt.* | -0,04 | 0,32% | -0,08 | 
| Strconv.* | 0 | 0,13% | -0,01 | 
| Sync.* | 0,05 | 0,79% | 0 | 
| container/list.* | 0,03 | 0,16% | 0,03 | 
| Strings.* | -0,01 | 0,29% | 0,06 | 
| Reflect.* | 0,11 | 1,30% | 0,37 | 
| internal/runtime/* | 0,17 | 2,64% | 0,38 | 
| encoding/json.* | 0,21 | 0,98% | 0,56 | 
| Net.* | 0,04 | 0,26% | 0,71 | 
| internal/*etc | 0,12 | 0,83% | 0,89 | 
| Syscall.* | 0,01 | 0,16% | 1 | 
| github.com/go-playground/validator.* | 0,04 | 0,51% | 1,14 | 
| github.com/jackc/pgx/v5.* | 0,05 | 2,70% | 2,39 | 
| database/sql.* | 0,02 | 1,15% | 3,33 | 

Top-20 худших и лучших результатов по 'flat':
| Функция | flat,s | flat% | сum, s | cum% |
| ------- | ------ | ----- | ------ | ---- |
| runtime.nanotime | -0,46 | 1,46% | -0,46 | 1,46% | 
| runtime.scanobject | -0,39 | 1,24% | -1,01 | 3,20% | 
| runtime.step | -0,14 | 0,44% | -0,14 | 0,44% | 
| time.runtimeNow | -0,14 | 0,44% | -0,14 | 0,44% | 
| runtime.(*gcBits).bitp | -0,13 | 0,41% | -0,13 | 0,41% | 
| runtime.findObject | -0,12 | 0,38% | -0,19 | 0,60% | 
| gorm.io/gorm.(*processor).Execute | -0,1 | 0,32% | -0,57 | 1,81% | 
| internal/runtime/maps.ctrlGroup.matchH2 | -0,09 | 0,29% | -0,09 | 0,29% | 
| runtime.(*mspan).heapBitsSmallForAddr | -0,09 | 0,29% | -0,09 | 0,29% | 
| encoding/json.(*decodeState).object | 0,06 | 0,19% | 0,07 | 0,22% | 
| encoding/json.checkValid | 0,06 | 0,19% | 0,11 | 0,35% | 
| github.com/jackc/pgx/v5/pgproto3.(*Frontend).Receive | 0,06 | 0,19% | 0,11 | 0,35% | 
| runtime.gopark | 0,07 | 0,22% | 0,07 | 0,22% | 
| runtime.selectgo | 0,08 | 0,25% | 0,05 | 0,16% | 
| runtime.memclrNoHeapPointers | 0,08 | 0,25% | 0,08 | 0,25% | 
| runtime.usleep | 0,08 | 0,25% | 0,08 | 0,25% | 
| runtime.stealWork | 0,08 | 0,25% | 0,25 | 0,79% | 
| gorm.io/gorm.(*Statement).clone | 0,09 | 0,29% | 0,12 | 0,38% | 
| internal/runtime/syscall.Syscall6 | 0,32 | 1,01% | 0,32 | 1,01% |

#### Diff Allocs

Преамбула:
```bash
File: orderservice
Build ID: 56fd7ab202b9ced3754b0933603c2d482033c547
Type: alloc_space
Time: 2026-04-10 20:31:27 CEST
Showing nodes accounting for 619058.82kB, 34.42% of 1798443.97kB total
Dropped 316 nodes (cum <= 8992.22kB)
```

Аггрегация по пакетам и рейтинг по 'cum':
| Пакет | flat, kB | flat% | сum, kB |
| ----- | ------- | ----- | ------ |
| Regexp.* | -24031,48 | 1,33% | -105311,95 | 
| database/sql.* | 512,01 | 0,03% | -64503,05 | 
| gorm.io/gorm.* | 25609,57 | 4,72% | -35973,1 | 
| github.com/jackc/pgx/v5.* | -10752,74 | 0,60% | -27137,86 | 
| Strings.* | -12803,65 | 0,72% | -17412,3 | 
| Reflect.* | -4608,78 | 0,26% | -13826,34 | 
| Bytes.* | 3812,67 | 0,21% | 11438,01 | 
| internal/sync.* | 0 | 0,00% | 12289,01 | 
| github.com/go | 15361,61 | 0,86% | 15929,8 | 
| sync.(*Map)&(*Pool) | 5644,39 | 0,31% | 30987,17 |
| orderservice/internal/* | -12612,57 | 0,75% | 551893,69 | 
| github.com/segmentio/kafka-go | 638048,91 | 36,04% | 2649914,32 | 


Top-20 худших и лучших результатов по 'flat':
| Функция | flat,kB | flat% | сum, kB | cum% |
| ------- | ------- | ----- | ------- | ---- |
| regexp.(*bitState).reset | -17886,84 | 0,99% | -17886,84 | 0,99% | 
| strings.(*Builder).WriteString | -8195 | 0,46% | -8195 | 0,46% | 
| gorm.io/gorm/utils.FileWithLineNum | -8193,38 | 0,46% | -8193,38 | 0,46% | 
| gorm.io/gorm/clause.Values.Build | -7168,44 | 0,40% | -9729,72 | 0,54% | 
| github.com/jackc/pgx/v5/stdlib.(*Rows).Next | -6144,13 | 0,34% | -8704,15 | 0,48% | 
| orderservice/internal/service.(*orderService).AddNewOrder | -5635,03 | 0,31% | -9207,17 | 0,51% | 
| context.(*cancelCtx).Done | -5120,55 | 0,28% | -5120,55 | 0,28% | 
| gorm.io/gorm.(*Statement).AddClause | -4612,72 | 0,26% | -11781,16 | 0,66% | 
| reflect.growslice | -4608,78 | 0,26% | -4608,78 | 0,26% | 
| strings.(*Builder).grow | -4608,65 | 0,26% | -4608,65 | 0,26% | 
| github.com/segmentio/kafka | 5120,43 | 0,28% | 7168,43 | 0,40% | 
| sync.(*Pool).pinSlow | 5644,39 | 0,31% | 5644,39 | 0,31% | 
| gorm.io/gorm/callbacks.saveAssociations | 6144,75 | 0,34% | 48656,57 | 2,71% | 
| gorm.io/gorm.(*Statement).clone | 6145,5 | 0,34% | 9729,83 | 0,54% | 
| gorm.io/gorm.(*Statement).BuildCondition | 6656,34 | 0,37% | 5632,3 | 0,31% | 
| github.com/go | 8192,75 | 0,46% | 7168,76 | 0,40% | 
| gorm.io/gorm/callbacks.ConvertToCreateValues | 8711,29 | 0,48% | 4102,82 | 0,23% | 
| github.com/segmentio/kafka | 10348,66 | 0,58% | 10348,66 | 0,58% | 
| gorm.io/gorm.(*DB).getInstance | 14339,87 | 0,80% | 27142,8 | 1,51% | 
| github.com/segmentio/kafka | 624629,34 | 34,73% | 624629,34 | 34,73% |

#### Diff Heap

```bash
File: orderservice
Build ID: 56fd7ab202b9ced3754b0933603c2d482033c547
Type: inuse_space
Time: 2026-04-10 20:31:02 CEST
Showing nodes accounting for 0.86MB, 11.28% of 7.66MB total
Dropped 2 nodes (cum <= 0.04MB)
```

#### Выводы

Итого имеем:
- CPU: нагрузка упала на 3,5%;
- GC: частота запуска стала почти в 3 раза меньше;
- heap: немного подрос - на +11%, но протечек нет;
- allocs: сильно выросли - на +34% - лидеры по потреблению kafka и gorm.

Батчинг разгрузил ЦП, но при этом увеличил потребление памяти - вместо единичных экземпляров структур теперь приложение оперирует в памяти сразу коллекцией.


### Оптимизация 3 - Validator/GORM

План изменений:
- вынести экземпляр validator в глобальный слой - создавать его только 1 раз в пакете, а не при каждом вызове функции сервиса по обработке сообщений кафки;
- сделать bulk-insert в repository;
- отказаться от FirstOrCreate в пользу Create, убрать двойную проверку на существование заказа перед его созданием;
- оптимизировать работу с Kafka - минимизировать переаллокации или же переиспользовать.

Ожидания:
- уменьшение потребления CPU,
- увеличение heap и уменьшение allocs,
- уменьшение нагрузки на GC.

Ссылка на коммит с оптимизацией: 


#### Результаты по GC

| Версия | GC runs | GC pause, ns |
| ------ | ------- | ------------ |
| Initial result | 989 | 166801750 |
| Opt. 1 result | 408 | 62218231 |
| Opt. 2 result | 146 | 28078875 |
| **Opt. 3 result** | 75 | 13686507 |

В сравнении с предыдущей версией кода GC начал запускаться почти в 2 раза реже, STW так же уменьшился в 2 раза.

#### Diff CPU

Преамбула:
```bash
File: orderservice
Build ID: 487c9495939a70d53cff9154f72999aaf40499a6
Type: cpu
Time: 2026-04-13 15:41:41 CEST
Duration: 120.07s, Total samples = 30.45s (25.36%)
Showing nodes accounting for -17.12s, 56.22% of 30.45s total
Dropped 814 nodes (cum <= 0.15s)
```

##### Top-10 по 'flat'
| Функция | flat,s | flat% | сum, s | cum% |
| ------- | ------ | ----- | ------ | ---- |
| internal/runtime/syscall.Syscall6 | -7,21 | 23,68% | -7,21 | 23,68% |
| runtime.nanotime | -1,62 | 5,32% | -1,62 | 5,32% |
| runtime.futex | -1,51 | 4,96% | -1,51 | 4,96% |
| time.runtimeNow | -1,08 | 3,55% | -1,08 | 3,55% |
| runtime.nextFreeFast | -0,42 | 1,38% | -0,42 | 1,38% |
| runtime.scanobject | -0,35 | 1,15% | -0,78 | 2,56% |
| runtime.(*mspan).writeHeapBitsSmall | -0,27 | 0,89% | -0,28 | 0,92% |
| runtime.usleep | -0,25 | 0,82% | -0,25 | 0,82% |
| runtime.memclrNoHeapPointers | -0,21 | 0,69% | -0,21 | 0,69% |
| runtime.stealWork | -0,21 | 0,69% | -0,66 | 2,17% |


##### Top-10 по 'cum'
| Функция | flat,s | flat% | сum, s | cum% |
| ------- | ------ | ----- | ------ | ---- |
| orderservice/internal/kafka.StartConsumer | 0 | 0,00% | -18,79 | 61,71% | 
| orderservice/internal/service.(*orderService).AddNewOrder | -0,01 | 0,03% | -18,6 | 61,08% | 
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1 | -0,02 | 0,07% | -14,36 | 47,16% | 
| orderservice/internal/repository.(*orderRepository).AddNewOrder | 0 | 0,00% | -14,36 | 47,16% | 
| gorm.io/gorm.(*processor).Execute | -0,02 | 0,07% | -13,16 | 43,22% | 
| gorm.io/gorm.(*DB).FirstOrCreate | 0 | 0,00% | -11,17 | 36,68% | 
| database/sql.withLock | -0,04 | 0,13% | -9,03 | 29,66% | 
| database/sql.(*DB).queryDC | -0,02 | 0,07% | -7,58 | 24,89% | 
| internal/runtime/syscall.Syscall6 | -7,21 | 23,68% | -7,21 | 23,68% | 
| gorm.io/gorm.(*DB).Create | 0 | 0,00% | -6,96 | 22,86% |

##### Выводы по cpu diff
При сохранении нагрузки приложение начало потреблять суммарно на 56% меньше процессорного времени.
Это подтверждает, что batching снизил:
- количество транзакций;
- количество SQL-запросов;
- накладные расходы ORM.

Оптимизация устранила значительную часть инфраструктурных затрат:
- syscalls;
- GC;
- runtime overhead.

В результате CPU-нагрузка сместилась от системных операций к полезной работе.


#### Diff Allocs

Преамбула:
```bash
File: orderservice
Build ID: 487c9495939a70d53cff9154f72999aaf40499a6
Type: alloc_space
Time: 2026-04-13 15:43:21 CEST
Showing nodes accounting for -1003.40MB, 42.36% of 2368.71MB total
Dropped 326 nodes (cum <= 11.84MB)
```

##### Top-10 по 'flat'
| Функция | flat,MB | flat% | сum, MB | cum% |
| ------- | ------- | ----- | ------- | ---- |
| gorm.io/gorm.(*Statement).clone | -220,12 | 9,29% | -245,62 | 10,37% | 
| gorm.io/gorm.(*Statement).AddClause | -163,1 | 6,89% | -188,6 | 7,96% | 
| github.com/jackc/pgx/v5.(*ExtendedQueryBuilder).appendParam | 75,6 | 3,19% | 124,23 | 5,24% | 
| github.com/go-playground/validator.(*Validate).extractStructCache | -71,5 | 3,02% | -186,51 | 7,87% | 
| github.com/segmentio/kafka-go.(*writeBatch).add | 70,75 | 2,99% | 70,75 | 2,99% | 
| github.com/go-playground/validator.New | -69,3 | 2,93% | -99,8 | 4,21% | 
| github.com/go-playground/validator.(*Validate).parseFieldTagsRecursive | -63,51 | 2,68% | -100,51 | 4,24% | 
| github.com/jackc/pgx/v5/pgproto3.(*Bind).Encode | 50,71 | 2,14% | 68,78 | 2,90% | 
| gorm.io/gorm.(*DB).Session | -49,01 | 2,07% | -143,56 | 6,06% | 
| gorm.io/gorm.Scan | -47,01 | 1,98% | -117,52 | 4,96% |

##### Top-10 по 'cum'
| Функция | flat,MB | flat% | сum, MB | cum% |
| ------- | ------- | ----- | ------- | ---- |
| orderservice/internal/kafka.StartConsumer | -3 | 0,13% | -1642,08 | 69,32% |
| orderservice/internal/service.(*orderService).AddNewOrder | -19,01 | 0,80% | -1634,08 | 68,99% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder.func1 | -13,5 | 0,57% | -1157,37 | 48,86% |
| orderservice/internal/repository.(*orderRepository).AddNewOrder | 0 | 0,00% | -1157,37 | 48,86% |
| gorm.io/gorm.(*DB).FirstOrCreate | -0,5 | 0,02% | -997,32 | 42,10% |
| gorm.io/gorm.(*processor).Execute | -3 | 0,13% | -581,35 | 24,54% |
| gorm.io/gorm/callbacks.RegisterDefaultCallbacks.SaveAfterAssociations.func4 | -0,5 | 0,02% | -401,9 | 16,97% |
| gorm.io/gorm/callbacks.saveAssociations | -6,5 | 0,27% | -393,9 | 16,63% |
| gorm.io/gorm.(*DB).Create | 0 | 0,00% | -352,32 | 14,87% |
| gorm.io/gorm.(*Statement).clone | -220,12 | 9,29% | -245,62 | 10,37% |

##### Выводы

Общий объём аллокаций снизился на ~42% (-1003MB), что связано с переходом на batch-обработку и уменьшением количества операций на единицу данных.

Основное снижение наблюдается в:
- GORM (~-430MB суммарно) - из-за уменьшения транзакций и ORM-вызовов за счёт batching
- Validator (~-200MB суммарно) - так как устранена повторная инициализация validator (вынесен в уровень пакета)

При этом увеличились аллокации:
- pgx: +~125MB
- kafka-go: +~70MB
Причина: увеличение размера батчей и объёма данных в рамках одной операции.

Итого:
- меньше мелких аллокаций;
- больше крупных батчевых аллокаций;
- снижение давления на GC.

Оптимизация перераспределила память в пользу более редких, но крупных операций.

#### Diff Heap

```bash
File: orderservice
Build ID: 487c9495939a70d53cff9154f72999aaf40499a6
Type: inuse_space
Time: 2026-04-13 15:43:45 CEST
Showing nodes accounting for 19100.33kB, 206.58% of 9245.97kB total
Dropped 5 nodes (cum <= 46.23kB)
```

Размер кучи вырос с 9Мб до 28Мб - что было ожидаемо из-за смены пайплайна обработки одиночных заказов на обработку батчей по 100 заказов.

## Итоги

При сравнении стартовых показателей с результатами последней оптимизации видно следующее:
1. потребление процессорного времени сократилось на -21.60s, что составляет 60% от изначального потребления;
2. аллокация памяти снизилась на -1495MB, что является 51% от изначального кол-ва аллокаций;
3. размер кучи в свою очередь подрос на ~19MB, то есть на 206.37% от изначальных 9MB.

В процессе оптимизаций было достигнуто смещение занятости ЦП от инфраструктурной/рантайм работы в сторону выполнения бизнес-задач.
