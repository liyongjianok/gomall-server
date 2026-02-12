/*
 * Go Mall 数据库全量初始化脚本 (Updated Version)
 * 更新记录:
 * 1. users 表新增 avatar 字段 (类型 MEDIUMTEXT)，用于存储 Base64 头像
 * 2. 包含之前的修复 (stock字段, total_amount, ID类型统一)
 */

SET NAMES utf8mb4;

SET FOREIGN_KEY_CHECKS = 0;

-- =======================================================
-- 1. 用户服务 (db_user)
-- =======================================================
DROP DATABASE IF EXISTS `db_user`;

CREATE DATABASE `db_user` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_user`;

-- 用户表
CREATE TABLE `users` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `username` varchar(255) NOT NULL,
    `password` varchar(255) NOT NULL,
    `mobile` varchar(20) DEFAULT NULL,
    `nickname` varchar(255) DEFAULT NULL,
    `avatar` MEDIUMTEXT DEFAULT NULL COMMENT '用户头像(Base64)', -- 🔥 新增字段，使用大文本存储图片
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_username` (`username`),
    KEY `idx_deleted_at` (`deleted_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- 插入测试账号
INSERT INTO
    `users` (
        `username`,
        `password`,
        `mobile`,
        `nickname`
    )
VALUES (
        'shouguang_03',
        '123',
        '13800138000',
        '测试管理员'
    );

-- 地址表
CREATE TABLE `addresses` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) NOT NULL,
    `name` varchar(50) DEFAULT NULL,
    `mobile` varchar(20) DEFAULT NULL,
    `province` varchar(50) DEFAULT NULL,
    `city` varchar(50) DEFAULT NULL,
    `district` varchar(50) DEFAULT NULL,
    `detail_address` varchar(255) DEFAULT NULL,
    `is_default` tinyint(1) DEFAULT 0,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_user_id` (`user_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- =======================================================
-- 2. 商品服务 (db_product)
-- =======================================================
DROP DATABASE IF EXISTS `db_product`;

CREATE DATABASE `db_product` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_product`;

-- 商品主表
CREATE TABLE `products` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `name` varchar(255) NOT NULL,
    `description` varchar(1000) DEFAULT NULL,
    `picture` varchar(255) DEFAULT NULL,
    `price` float(10, 2) NOT NULL,
    `stock` int(11) DEFAULT 1000 COMMENT '库存',
    `category_id` int(11) DEFAULT '0',
    PRIMARY KEY (`id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- SKU表
CREATE TABLE `skus` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `product_id` bigint(20) NOT NULL,
    `name` varchar(255) DEFAULT NULL,
    `price` float(10, 2) DEFAULT NULL,
    `stock` int(11) DEFAULT 1000 COMMENT '库存',
    `picture` varchar(255) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_product_id` (`product_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- 插入商品数据
INSERT INTO
    `products` (
        `name`,
        `description`,
        `picture`,
        `price`,
        `stock`,
        `category_id`
    )
VALUES
    -- 蔬菜
    (
        '新鲜西红柿',
        '农家自种有机西红柿，酸甜可口',
        'https://placehold.co/300x300/ff6347/ffffff?text=Tomato',
        5.50,
        999,
        1
    ),
    (
        '五彩甜椒',
        '富含维生素，色彩鲜艳',
        'https://placehold.co/300x300/ffa500/ffffff?text=Pepper',
        8.80,
        800,
        1
    ),
    (
        '高山西兰花',
        '绿色健康，健身首选',
        'https://placehold.co/300x300/228b22/ffffff?text=Broccoli',
        6.00,
        500,
        1
    ),
    (
        '精选土豆',
        '适合炖肉，粉糯香甜',
        'https://placehold.co/300x300/d2b48c/ffffff?text=Potato',
        3.20,
        2000,
        1
    ),
    (
        '紫茄子',
        '皮薄肉嫩，烧烤必备',
        'https://placehold.co/300x300/800080/ffffff?text=Eggplant',
        4.50,
        600,
        1
    ),
    (
        '大白菜',
        '冬日必备，清甜爽口',
        'https://placehold.co/300x300/90ee90/ffffff?text=Cabbage',
        2.00,
        3000,
        1
    ),
    (
        '胡萝卜',
        '护眼明目，营养丰富',
        'https://placehold.co/300x300/ff4500/ffffff?text=Carrot',
        3.00,
        1500,
        1
    ),
    (
        '鲜嫩黄瓜',
        '凉拌神器，脆爽解腻',
        'https://placehold.co/300x300/32cd32/ffffff?text=Cucumber',
        4.00,
        1200,
        1
    ),
    (
        '菠菜',
        '绿叶蔬菜，营养均衡',
        'https://placehold.co/300x300/006400/ffffff?text=Spinach',
        5.00,
        400,
        1
    ),
    (
        '板栗南瓜',
        '粉糯香甜，口感像板栗',
        'https://placehold.co/300x300/e9967a/ffffff?text=Pumpkin',
        4.20,
        800,
        1
    ),
    -- 水果
    (
        '红富士苹果',
        '烟台产地直供，脆甜多汁',
        'https://placehold.co/300x300/dc143c/ffffff?text=Apple',
        8.90,
        1000,
        2
    ),
    (
        '香蕉',
        '软糯香甜，补充能量',
        'https://placehold.co/300x300/ffd700/ffffff?text=Banana',
        6.50,
        200,
        2
    ),
    (
        '巨峰葡萄',
        '颗颗饱满，酸甜适中',
        'https://placehold.co/300x300/8a2be2/ffffff?text=Grape',
        12.80,
        100,
        2
    ),
    (
        '麒麟西瓜',
        '皮薄肉厚，沙瓤多汁',
        'https://placehold.co/300x300/2e8b57/ffffff?text=Watermelon',
        2.50,
        50,
        2
    ),
    (
        '丹东草莓',
        '牛奶草莓，香气浓郁',
        'https://placehold.co/300x300/ff69b4/ffffff?text=Strawberry',
        25.00,
        80,
        2
    ),
    (
        '脐橙',
        '果肉细腻，汁多味甜',
        'https://placehold.co/300x300/ff8c00/ffffff?text=Orange',
        9.90,
        600,
        2
    ),
    -- 肉蛋
    (
        '肥牛卷',
        '火锅专用，纹理清晰',
        'https://placehold.co/300x300/a52a2a/ffffff?text=Beef',
        45.00,
        200,
        3
    ),
    (
        '五花肉',
        '肥瘦相间，红烧肉专用',
        'https://placehold.co/300x300/f08080/ffffff?text=Pork',
        28.50,
        300,
        3
    ),
    (
        '土鸡蛋',
        '蛋黄黄亮，营养价值高',
        'https://placehold.co/300x300/f4a460/ffffff?text=Egg',
        15.80,
        1000,
        3
    ),
    (
        '大虾',
        '个大饱满，鲜甜Q弹',
        'https://placehold.co/300x300/ff7f50/ffffff?text=Shrimp',
        58.00,
        150,
        3
    ),
    -- 粮油
    (
        '五常大米',
        '米香浓郁，软糯回甘',
        'https://placehold.co/300x300/fffaf0/000000?text=Rice',
        69.90,
        500,
        4
    ),
    (
        '食用油',
        '物理压榨，健康首选',
        'https://placehold.co/300x300/daa520/ffffff?text=Oil',
        75.00,
        400,
        4
    );

-- 自动同步 SKU
INSERT INTO
    `skus` (
        product_id,
        name,
        price,
        stock,
        picture
    )
SELECT id, name, price, stock, picture
FROM `products`;

-- =======================================================
-- 3. 订单服务 (db_order)
-- =======================================================
DROP DATABASE IF EXISTS `db_order`;

CREATE DATABASE `db_order` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_order`;

-- 订单表
CREATE TABLE `orders` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `order_no` varchar(64) NOT NULL,
    `user_id` bigint(20) NOT NULL,
    `total_amount` float(10, 2) NOT NULL DEFAULT 0.00 COMMENT '总金额',
    `status` int(11) DEFAULT '0' COMMENT '0:未支付 1:已支付 2:已取消',
    `address_id` bigint(20) DEFAULT NULL,
    `receiver_name` varchar(64) DEFAULT '',
    `receiver_mobile` varchar(20) DEFAULT '',
    `receiver_address` varchar(255) DEFAULT '',
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_order_no` (`order_no`),
    KEY `idx_user_id` (`user_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- 订单详情表
CREATE TABLE `order_items` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `order_id` bigint(20) NOT NULL,
    `order_no` varchar(64) DEFAULT NULL,
    `sku_id` bigint(20) NOT NULL,
    `name` varchar(255) DEFAULT NULL,
    `price` float(10, 2) DEFAULT NULL,
    `quantity` int(11) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_order_id` (`order_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- =======================================================
-- 4. 购物车服务 (db_cart)
-- =======================================================
DROP DATABASE IF EXISTS `db_cart`;

CREATE DATABASE `db_cart` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_cart`;

CREATE TABLE `cart_items` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) NOT NULL,
    `sku_id` bigint(20) NOT NULL,
    `quantity` int(11) DEFAULT '1',
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_user_sku` (`user_id`, `sku_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

SET FOREIGN_KEY_CHECKS = 1;

-- =======================================================
-- 5. 评价服务 (db_review)
-- =======================================================
CREATE DATABASE IF NOT EXISTS `db_review` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_review`;

CREATE TABLE `reviews` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) NOT NULL COMMENT '用户ID',
    `order_no` varchar(64) NOT NULL COMMENT '订单号',
    `sku_id` bigint(20) NOT NULL COMMENT '商品SKU ID',
    `product_id` bigint(20) NOT NULL COMMENT '商品SPU ID (冗余字段方便查询)',
    `content` text COMMENT '评价内容',
    `images` json DEFAULT NULL COMMENT '评价图片(JSON数组)',
    `star` tinyint(1) NOT NULL DEFAULT 5 COMMENT '评分 1-5',
    `is_anonymous` tinyint(1) DEFAULT 0 COMMENT '是否匿名',
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    -- 核心约束：一个订单里的同一个商品，只能评价一次
    UNIQUE KEY `uni_order_sku` (`order_no`, `sku_id`),
    -- 索引：查询某个商品的评价列表
    KEY `idx_product_id` (`product_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;