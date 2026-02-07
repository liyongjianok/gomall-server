-- ==========================================
-- 1. 初始化用户数据库
-- ==========================================
CREATE DATABASE IF NOT EXISTS db_user DEFAULT CHARACTER SET utf8mb4;

USE db_user;

CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    mobile VARCHAR(20),
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_deleted_at (deleted_at)
);

-- ==========================================
-- 2. 初始化商品数据库
-- ==========================================
CREATE DATABASE IF NOT EXISTS db_product DEFAULT CHARACTER SET utf8mb4;

USE db_product;

CREATE TABLE IF NOT EXISTS categories (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    parent_id BIGINT UNSIGNED DEFAULT 0,
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_deleted_at (deleted_at)
);

CREATE TABLE IF NOT EXISTS products (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    category_id BIGINT UNSIGNED NOT NULL,
    picture VARCHAR(255),
    price DECIMAL(10, 2) NOT NULL,
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_deleted_at (deleted_at)
);

CREATE TABLE IF NOT EXISTS skus (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    product_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(100) NOT NULL,
    price DECIMAL(10, 2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    picture VARCHAR(255),
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_deleted_at (deleted_at)
);

-- 插入测试数据
INSERT INTO categories (id, name, parent_id) VALUES (1, '新鲜蔬菜', 0);

INSERT INTO
    products (
        id,
        name,
        description,
        category_id,
        price
    )
VALUES (
        1,
        '寿光水果黄瓜',
        '口感清脆甘甜',
        1,
        15.00
    );

INSERT INTO
    skus (
        product_id,
        name,
        price,
        stock
    )
VALUES (1, '3斤尝鲜装', 19.90, 500);

-- ==========================================
-- 3. 初始化订单数据库
-- ==========================================
CREATE DATABASE IF NOT EXISTS db_order DEFAULT CHARACTER SET utf8mb4;

USE db_order;

CREATE TABLE IF NOT EXISTS orders (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_no VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT UNSIGNED NOT NULL,
    total_amount DECIMAL(10, 2) NOT NULL,
    status INT NOT NULL DEFAULT 0,
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_user_id (user_id)
);

CREATE TABLE IF NOT EXISTS order_items (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT UNSIGNED NOT NULL,
    product_id BIGINT UNSIGNED NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    product_name VARCHAR(100) NOT NULL,
    sku_name VARCHAR(100) NOT NULL,
    price DECIMAL(10, 2) NOT NULL,
    quantity INT NOT NULL,
    picture VARCHAR(255),
    created_at DATETIME(3),
    updated_at DATETIME(3),
    deleted_at DATETIME(3),
    INDEX idx_order_id (order_id)
);