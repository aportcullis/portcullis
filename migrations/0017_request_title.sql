-- Titles remain visible metadata; bodies stay in the encrypted payload.
alter table public.access_requests
    add column title text not null default '',
    add constraint access_requests_title_valid check (
        char_length(title) <= 200
        and position(chr(10) in title) = 0
        and position(chr(13) in title) = 0
    );
