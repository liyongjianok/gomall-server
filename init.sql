/*
 * Go Mall 数据库全量初始化脚本
 * 包含: db_product, db_user, db_order, db_cart
 * 注意: 执行此脚本会清空所有现有数据！
 */

SET NAMES utf8mb4;

SET FOREIGN_KEY_CHECKS = 0;

-- =======================================================
-- 1. 商品服务数据库 (db_product)
-- =======================================================
DROP DATABASE IF EXISTS `db_product`;

CREATE DATABASE `db_product` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_product`;

CREATE TABLE `products` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT COMMENT '商品ID',
    `name` varchar(255) NOT NULL COMMENT '商品名称',
    `description` varchar(1000) DEFAULT NULL COMMENT '商品描述',
    `picture` varchar(255) DEFAULT NULL COMMENT '商品图片URL',
    `price` float(10, 2) NOT NULL COMMENT '商品价格',
    `category_id` int(11) DEFAULT '0' COMMENT '分类ID: 1蔬菜 2水果 3肉蛋 4粮油',
    PRIMARY KEY (`id`)
) ENGINE = InnoDB AUTO_INCREMENT = 1 DEFAULT CHARSET = utf8mb4;

-- 插入 22 条测试数据
INSERT INTO
    `products` (
        `name`,
        `description`,
        `picture`,
        `price`,
        `category_id`
    )
VALUES
    -- 蔬菜类 (Category 1)
    (
        '新鲜西红柿',
        '农家自种有机西红柿，酸甜可口，炒蛋首选',
        'https://placehold.co/300x300/ff6347/ffffff?text=Tomato',
        5.50,
        1
    ),
    (
        '五彩甜椒',
        '富含维生素，色彩鲜艳，口感清脆',
        'https://placehold.co/300x300/ffa500/ffffff?text=Pepper',
        8.80,
        1
    ),
    (
        '高山西兰花',
        '绿色健康，健身减脂首选蔬菜',
        'https://placehold.co/300x300/228b22/ffffff?text=Broccoli',
        6.90,
        1
    ),
    (
        '精选土豆',
        '黄心土豆，适合炖牛腩，粉糯香甜',
        'https://placehold.co/300x300/d2b48c/ffffff?text=Potato',
        3.20,
        1
    ),
    (
        '紫皮长茄子',
        '皮薄肉嫩，适合红烧或烧烤',
        'https://placehold.co/300x300/800080/ffffff?text=Eggplant',
        4.50,
        1
    ),
    (
        '大白菜',
        '冬日必备储备菜，清甜爽口',
        'https://placehold.co/300x300/90ee90/ffffff?text=Cabbage',
        1.99,
        1
    ),
    (
        '带泥胡萝卜',
        '护眼明目，营养丰富，煲汤神器',
        'https://placehold.co/300x300/ff4500/ffffff?text=Carrot',
        2.80,
        1
    ),
    (
        '顶花黄瓜',
        '凉拌神器，清脆爽口解油腻',
        'https://placehold.co/300x300/32cd32/ffffff?text=Cucumber',
        3.50,
        1
    ),
    (
        '有机菠菜',
        '大力水手最爱，补铁补血',
        'https://placehold.co/300x300/006400/ffffff?text=Spinach',
        5.00,
        1
    ),
    (
        '云南小南瓜',
        '板栗味南瓜，软糯香甜',
        'https://placehold.co/300x300/e9967a/ffffff?text=Pumpkin',
        4.20,
        1
    ),

-- 水果类 (Category 2)
(
    '红富士苹果',
    '烟台红富士，脆甜多汁',
    'https://placehold.co/300x300/dc143c/ffffff?text=Apple',
    8.90,
    2
),
(
    '进口香蕉',
    '软糯香甜，补充能量',
    'https://placehold.co/300x300/ffd700/ffffff?text=Banana',
    6.50,
    2
),
(
    '巨峰葡萄',
    '颗颗饱满，酸甜适中',
    'https://placehold.co/300x300/8a2be2/ffffff?text=Grape',
    12.80,
    2
),
(
    '海南西瓜',
    '皮薄肉厚，沙瓤多汁，解暑神器',
    'https://placehold.co/300x300/2e8b57/ffffff?text=Watermelon',
    2.50,
    2
),
(
    '丹东草莓',
    '牛奶草莓，香气浓郁',
    'https://placehold.co/300x300/ff69b4/ffffff?text=Strawberry',
    25.00,
    2
),
(
    '赣南脐橙',
    '果肉细腻，汁多味甜',
    'https://placehold.co/300x300/ff8c00/ffffff?text=Orange',
    9.90,
    2
),

-- 肉蛋类 (Category 3)
(
    '谷饲牛肉卷',
    '适合火锅，纹理清晰，肉质鲜嫩',
    'https://placehold.co/300x300/a52a2a/ffffff?text=Beef',
    45.00,
    3
),
(
    '精修五花肉',
    '肥瘦相间，红烧肉专用',
    'https://placehold.co/300x300/f08080/ffffff?text=Pork',
    28.50,
    3
),
(
    '散养土鸡蛋',
    '蛋黄黄亮，营养价值高',
    'https://placehold.co/300x300/f4a460/ffffff?text=Egg',
    15.80,
    3
),
(
    '深海大虾',
    '个大饱满，鲜甜Q弹',
    'https://placehold.co/300x300/ff7f50/ffffff?text=Shrimp',
    58.00,
    3
),

-- 粮油类 (Category 4)
(
    '五常大米',
    '米香浓郁，软糯回甘',
    'https://placehold.co/300x300/fffaf0/000000?text=Rice',
    69.90,
    4
),
(
    '金龙鱼调和油',
    '营养均衡，煎炒烹炸皆宜',
    'https://placehold.co/300x300/daa520/ffffff?text=Oil',
    75.00,
    4
);

-- =======================================================
-- 2. 用户服务数据库 (db_user)
-- =======================================================
DROP DATABASE IF EXISTS `db_user`;

CREATE DATABASE `db_user` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_user`;

CREATE TABLE `users` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `username` varchar(255) NOT NULL,
    `password` varchar(255) NOT NULL COMMENT '如果是明文比对直接存，如果是加密请存Hash',
    `mobile` varchar(20) DEFAULT NULL,
    `nickname` varchar(255) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_username` (`username`),
    KEY `idx_deleted_at` (`deleted_at`)
) ENGINE = InnoDB AUTO_INCREMENT = 1 DEFAULT CHARSET = utf8mb4;

-- 插入默认测试用户 (密码: 123)
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

-- 地址表 (结构已建立，数据留空待手动添加)
CREATE TABLE `addresses` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) NOT NULL,
    `name` varchar(50) DEFAULT NULL COMMENT '收货人姓名',
    `mobile` varchar(20) DEFAULT NULL COMMENT '收货人电话',
    `province` varchar(50) DEFAULT NULL,
    `city` varchar(50) DEFAULT NULL,
    `district` varchar(50) DEFAULT NULL,
    `detail_address` varchar(255) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at` datetime DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_user_id` (`user_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- =======================================================
-- 3. 订单服务数据库 (db_order)
-- =======================================================
DROP DATABASE IF EXISTS `db_order`;

CREATE DATABASE `db_order` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_order`;

CREATE TABLE `orders` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `order_no` varchar(64) NOT NULL COMMENT '订单号',
    `user_id` bigint(20) NOT NULL,
    `amount` float(10, 2) NOT NULL COMMENT '总金额',
    `status` int(11) DEFAULT '0' COMMENT '0:未支付 1:已支付 2:已取消',
    `address_id` bigint(20) DEFAULT NULL,
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
    `sku_id` bigint(20) NOT NULL,
    `name` varchar(255) DEFAULT NULL,
    `price` float(10, 2) DEFAULT NULL,
    `quantity` int(11) DEFAULT NULL,
    `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_order_id` (`order_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- =======================================================
-- 4. 购物车服务数据库 (db_cart)
-- =======================================================
DROP DATABASE IF EXISTS `db_cart`;

CREATE DATABASE `db_cart` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

USE `db_cart`;

-- 如果购物车使用纯 Redis，此表可能不会被使用，但建立结构以防万一
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