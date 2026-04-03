-- Таблица заказов
CREATE TABLE orders (
    order_uid TEXT PRIMARY KEY,
    track_number TEXT NOT NULL,
    entry TEXT NOT NULL,
    locale TEXT NOT NULL,
    internal_signature TEXT NOT NULL,
    customer_id TEXT NOT NULL,
    delivery_service TEXT NOT NULL,
    shardkey TEXT NOT NULL,
    sm_id INT NOT NULL,
    date_created TEXT NOT NULL,
    oof_shard TEXT NOT NULL
);

-- Таблица доставок
CREATE TABLE deliveries (
    did SERIAL PRIMARY KEY,
    order_uid TEXT NOT NULL,
    name TEXT NOT NULL,
    phone TEXT NOT NULL,
    zip TEXT NOT NULL,
    city TEXT NOT NULL,
    address TEXT NOT NULL,
    region TEXT NOT NULL,
    email TEXT NOT NULL,
    CONSTRAINT fk_deliveries_order FOREIGN KEY (order_uid) REFERENCES orders (order_uid) ON UPDATE CASCADE ON DELETE CASCADE
);

-- Таблица оплат
CREATE TABLE payments (
    pid SERIAL PRIMARY KEY,
    order_uid TEXT NOT NULL,
    transaction TEXT NOT NULL,
    request_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    provider TEXT NOT NULL,
    amount INT NOT NULL CHECK (amount >= 1),
    payment_dt BIGINT NOT NULL CHECK (payment_dt >= 1), -- unix time
    bank TEXT NOT NULL,
    delivery_cost INT NOT NULL CHECK (delivery_cost >= 0),
    goods_total INT NOT NULL CHECK (goods_total >= 1),
    custom_fee INT NOT NULL CHECK (custom_fee >= 0),
    CONSTRAINT fk_payments_order FOREIGN KEY (order_uid) REFERENCES orders (order_uid) ON UPDATE CASCADE ON DELETE CASCADE
);

-- Таблица товаров
CREATE TABLE items (
    iid SERIAL PRIMARY KEY,
    order_uid TEXT NOT NULL,
    chrt_id INT NOT NULL CHECK (chrt_id >= 1),
    track_number TEXT NOT NULL,
    price INT NOT NULL CHECK (price >= 1),
    rid TEXT NOT NULL,
    name TEXT NOT NULL,
    sale INT NOT NULL CHECK (
        sale >= 0
        AND sale <= 100
    ),
    size TEXT NOT NULL,
    total_price INT NOT NULL CHECK (total_price >= 1),
    nm_id INT NOT NULL CHECK (nm_id >= 1),
    brand TEXT NOT NULL,
    status INT NOT NULL CHECK (status >= 0),
    CONSTRAINT fk_items_order FOREIGN KEY (order_uid) REFERENCES orders (order_uid) ON UPDATE CASCADE ON DELETE CASCADE
);