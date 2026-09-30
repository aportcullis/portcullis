-- Create compose’s restricted app login before migration grants runtime membership. Init scripts run only on an empty data volume; production provisions its own login.
create user portcullis_app password 'portcullis';
