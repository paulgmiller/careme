from wiopy import WalmartIO
walmart_io = WalmartIO(
    private_key_version="1",
    private_key_filename="../../keys/walmart_prod",
    consumer_id='52dae855-d02f-488b-b179-1df6700d7dcf',
)
#data = walmart_io.stores(zip=98007) #lat=29.735577, lon=-95.511747)
data = walmart_io.product_lookup('33093101')[0]
print(data)