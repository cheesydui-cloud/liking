UPDATE inbounds
SET settings = json_set(settings, '$.fingerprint', 'safari')
WHERE json_valid(settings)
  AND json_type(settings) = 'object'
  AND json_extract(settings, '$.fingerprint') = 'chrome';
