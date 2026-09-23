-- speed_limit was whole Mbps. 1 Mbps = 125 KB/s, matching the nft conversion.
UPDATE users SET speed_limit = speed_limit * 125 WHERE speed_limit > 0;
UPDATE packages SET speed_limit = speed_limit * 125 WHERE speed_limit > 0;
