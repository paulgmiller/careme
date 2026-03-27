navigate(input.url);

// Wait for the main search grid to load
const grid_selector = '.pc-grid';
wait(grid_selector, {timeout: 60000});

// Wait for product items to appear
const product_selector = 'product-item-al-v2';
wait(product_selector, {timeout: 60000});

// Use load_more to handle infinite scroll
// The container is the pc-grid element
load_more(grid_selector, {timeout: 60000});

// Parse the page to get product URLs
const {product_urls} = parse();

console.log(`Found ${product_urls.length} products`);

if (!product_urls || product_urls.length === 0) {
    throw new Error('No products found on the page');
}

// Collect URLs using next_stage
for (let url of product_urls) {
    next_stage({url});
}
