/*
 * Go Mall 数据库全量初始化脚本 (Shouguang Veggie Special Edition)
 * 包含 32 种寿光特色蔬菜水果测试数据
 */

SET NAMES utf8mb4;

SET FOREIGN_KEY_CHECKS = 0;

-- =======================================================
-- 1. 用户服务 (db_user)
-- =======================================================
DROP DATABASE IF EXISTS `db_user`;

CREATE DATABASE `db_user` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_user`;

CREATE TABLE `users` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `username` varchar(255) NOT NULL,
    `password` varchar(255) NOT NULL,
    `mobile` varchar(20) DEFAULT NULL,
    `nickname` varchar(255) DEFAULT NULL,
    `avatar` MEDIUMTEXT DEFAULT NULL COMMENT '用户头像(Base64)',
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_username` (`username`),
    KEY `idx_deleted_at` (`deleted_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

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
        '13666668888',
        '寿光老乡'
    );

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

CREATE TABLE `products` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `name` varchar(255) NOT NULL,
    `description` varchar(1000) DEFAULT NULL,
    `picture` varchar(255) DEFAULT NULL,
    `price` float(10, 2) NOT NULL,
    `stock` int(11) DEFAULT 1000,
    `category_id` int(11) DEFAULT '0',
    PRIMARY KEY (`id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE `skus` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `product_id` bigint(20) NOT NULL,
    `name` varchar(255) DEFAULT NULL,
    `price` float(10, 2) DEFAULT NULL,
    `stock` int(11) DEFAULT 1000,
    `picture` varchar(255) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_product_id` (`product_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- 插入 32 种寿光蔬菜水果
INSERT INTO
    `products` (
        `id`,
        `name`,
        `description`,
        `picture`,
        `price`,
        `stock`,
        `category_id`
    )
VALUES (
        1,
        '寿光粉红西红柿',
        '皮薄汁多，沙瓤口感，儿时的味道',
        'https://placehold.co/300x300/ff6347/ffffff?text=Tomato',
        5.80,
        500,
        1
    ),
    (
        2,
        '三元朱黄瓜',
        '寿光冬暖式大棚发源地特产，脆爽清香',
        'https://placehold.co/300x300/32cd32/ffffff?text=Cucumber',
        4.50,
        800,
        1
    ),
    (
        3,
        '带泥胡萝卜',
        '寿光北部盐碱地种植，甜度极高',
        'https://placehold.co/300x300/ff4500/ffffff?text=Carrot',
        2.80,
        1000,
        1
    ),
    (
        4,
        '寿光独根红韭菜',
        '叶片肥厚，辛香味浓，包饺子绝配',
        'https://placehold.co/300x300/006400/ffffff?text=Leek',
        6.50,
        300,
        1
    ),
    (
        5,
        '五彩甜椒',
        '色彩艳丽，肉厚口感脆甜，适合凉拌',
        'https://placehold.co/300x300/ffa500/ffffff?text=Pepper',
        8.90,
        400,
        1
    ),
    (
        6,
        '高山圆包菜',
        '叶球紧实，脆嫩爽口，耐储存',
        'https://placehold.co/300x300/90ee90/ffffff?text=Cabbage',
        1.50,
        2000,
        1
    ),
    (
        7,
        '大葱(章丘引种)',
        '寿光改良种植，葱白长，味微甜',
        'https://placehold.co/300x300/f5f5f5/228b22?text=Scallion',
        3.20,
        1200,
        1
    ),
    (
        8,
        '紫皮大蒜',
        '个大瓣匀，蒜味浓郁，杀菌力强',
        'https://placehold.co/300x300/fffafa/a52a2a?text=Garlic',
        5.00,
        1500,
        1
    ),
    (
        9,
        '长茄子',
        '皮薄肉嫩，吸油少，适合红烧',
        'https://placehold.co/300x300/800080/ffffff?text=Eggplant',
        3.80,
        600,
        1
    ),
    (
        10,
        '线椒',
        '中辣口味，皮薄肉厚，下饭神器',
        'https://placehold.co/300x300/228b22/ffffff?text=Chili',
        7.20,
        500,
        1
    ),
    (
        11,
        '西葫芦',
        '鲜嫩多汁，适合炒蛋或做馅',
        'https://placehold.co/300x300/98fb98/006400?text=Zucchini',
        2.50,
        900,
        1
    ),
    (
        12,
        '寿光羊角蜜',
        '寿光特色甜瓜，甜度爆表，嘎嘣脆',
        'https://placehold.co/300x300/fffacd/8b4513?text=Melon',
        12.80,
        200,
        2
    ),
    (
        13,
        '板栗南瓜',
        '口感软糯，自带板栗清香',
        'https://placehold.co/300x300/e9967a/ffffff?text=Pumpkin',
        4.20,
        700,
        1
    ),
    (
        14,
        '西兰花',
        '花球青翠紧致，健身餐常备',
        'https://placehold.co/300x300/228b22/ffffff?text=Broccoli',
        6.80,
        400,
        1
    ),
    (
        15,
        '荷兰土豆',
        '表面光滑，口感粉糯，适合油炸炖煮',
        'https://placehold.co/300x300/d2b48c/ffffff?text=Potato',
        2.20,
        3000,
        1
    ),
    (
        16,
        '黑皮冬瓜',
        '肉厚耐煮，清热解暑，煲汤良品',
        'https://placehold.co/300x300/2f4f4f/ffffff?text=Melon',
        1.80,
        800,
        1
    ),
    (
        17,
        '苦瓜',
        '清火解腻，大棚精品，品相极佳',
        'https://placehold.co/300x300/00ff00/006400?text=Bitter',
        4.80,
        300,
        1
    ),
    (
        18,
        '山药(细毛山药)',
        '滋补佳品，软糯香甜',
        'https://placehold.co/300x300/f5deb3/8b4513?text=Yam',
        9.50,
        400,
        1
    ),
    (
        19,
        '白萝卜',
        '水分充足，清甜无渣，适合煲汤',
        'https://placehold.co/300x300/ffffff/000000?text=Radish',
        1.20,
        2500,
        1
    ),
    (
        20,
        '金针菇',
        '菌盖紧凑，口感顺滑',
        'https://placehold.co/300x300/fff5ee/deb887?text=Mushroom',
        3.50,
        600,
        1
    ),
    (
        21,
        '平菇',
        '鲜味十足，肉质厚实',
        'https://placehold.co/300x300/dcdcdc/696969?text=Mushroom',
        5.50,
        400,
        1
    ),
    (
        22,
        '圣女果(小西红柿)',
        '红亮如珠，一口一个甜滋滋',
        'https://placehold.co/300x300/ff0000/ffffff?text=CherryT',
        8.00,
        300,
        1
    ),
    (
        23,
        '生菜',
        '无土栽培，鲜嫩翠绿，烤肉伴侣',
        'https://placehold.co/300x300/7cfc00/006400?text=Lettuce',
        4.00,
        500,
        1
    ),
    (
        24,
        '油麦菜',
        '清香微苦，口感爽脆',
        'https://placehold.co/300x300/32cd32/ffffff?text=Leafy',
        3.00,
        600,
        1
    ),
    (
        25,
        '芦笋',
        '蔬菜之王，营养丰富，西餐常用',
        'https://placehold.co/300x300/006400/ffffff?text=Asparagus',
        15.00,
        200,
        1
    ),
    (
        26,
        '秋葵',
        '翠绿多汁，口感独特',
        'https://placehold.co/300x300/228b22/ffffff?text=Okra',
        9.80,
        300,
        1
    ),
    (
        27,
        '莲藕',
        '白净脆嫩，清甜爽口',
        'https://placehold.co/300x300/fff8dc/8b4513?text=Lotus',
        5.20,
        600,
        1
    ),
    (
        28,
        '贝贝南瓜',
        '个头小巧，甜度高，像红薯一样面',
        'https://placehold.co/300x300/556b2f/ffffff?text=Pumpkin',
        6.00,
        500,
        1
    ),
    (
        29,
        '扁豆',
        '农家品种，豆味浓郁',
        'https://placehold.co/300x300/8fbc8f/ffffff?text=Bean',
        5.60,
        400,
        1
    ),
    (
        30,
        '四季豆',
        '脆嫩无筋，怎么做都好吃',
        'https://placehold.co/300x300/00ff7f/ffffff?text=Bean',
        6.20,
        400,
        1
    ),
    (
        31,
        '娃娃菜',
        '精选心部，清甜鲜嫩',
        'https://placehold.co/300x300/fffff0/bdb76b?text=Cabbage',
        4.50,
        1000,
        1
    ),
    (
        32,
        '红薯',
        '寿光沙地种植，流油糯香',
        'https://placehold.co/300x300/cd853f/ffffff?text=Potato',
        3.50,
        2000,
        1
    );

INSERT INTO
    `skus` (
        id,
        product_id,
        name,
        price,
        stock,
        picture
    )
SELECT id, id, name, price, stock, picture
FROM `products`;

-- =======================================================
-- 3. 订单服务 (db_order)
-- =======================================================
DROP DATABASE IF EXISTS `db_order`;

CREATE DATABASE `db_order` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_order`;

CREATE TABLE `orders` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `order_no` varchar(64) NOT NULL,
    `user_id` bigint(20) NOT NULL,
    `total_amount` float(10, 2) NOT NULL DEFAULT 0.00,
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

CREATE TABLE `order_items` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `order_id` bigint(20) NOT NULL,
    `order_no` varchar(64) DEFAULT NULL,
    `product_id` bigint(20) NOT NULL,
    `sku_id` bigint(20) NOT NULL,
    `product_name` varchar(255) DEFAULT NULL,
    `sku_name` varchar(255) DEFAULT NULL,
    `price` float(10, 2) DEFAULT NULL,
    `quantity` int(11) DEFAULT NULL,
    `picture` varchar(255) DEFAULT NULL,
    `is_reviewed` tinyint(1) DEFAULT 0,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_order_id` (`order_id`),
    KEY `idx_order_no` (`order_no`)
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

-- =======================================================
-- 5. 评价服务 (db_review)
-- =======================================================
DROP DATABASE IF EXISTS `db_review`;

CREATE DATABASE `db_review` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_review`;

CREATE TABLE `reviews` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) NOT NULL,
    `order_no` varchar(64) NOT NULL,
    `sku_id` bigint(20) NOT NULL,
    `product_id` bigint(20) NOT NULL,
    `content` text,
    `images` json DEFAULT NULL,
    `star` tinyint(1) NOT NULL DEFAULT 5,
    `is_anonymous` tinyint(1) DEFAULT 0,
    `user_nickname` varchar(255) DEFAULT NULL,
    `user_avatar` MEDIUMTEXT DEFAULT NULL,
    `sku_name` varchar(255) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_order_sku` (`order_no`, `sku_id`),
    KEY `idx_product_id` (`product_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

SET FOREIGN_KEY_CHECKS = 1;