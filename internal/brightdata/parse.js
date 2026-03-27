// Extract product URLs from the page
const base_url = location.href;

// Acme Markets uses product-item-al-v2 elements with links inside
// The links have the pattern /shop/product-details.{id}.html
const product_links = $('a[href*="/shop/product-details"]').toArray();

console.log(`Found ${product_links.length} product links`);

// Use a Set to avoid duplicates
const product_url_set = new Set();

product_links.forEach(link => {
    const href = $(link).attr('href');
    
    // Validate the href
    if (href && 
        !href.includes('javascript:') && 
        !href.startsWith('#') &&
        href.includes('/shop/product-details')) {
        
        // Convert relative URLs to absolute
        try {
            const absolute_url = new URL(href, base_url).href;
            product_url_set.add(absolute_url);
        } catch (e) {
            console.log(`Invalid URL: ${href}`);
        }
    }
});

// Convert Set to Array
const product_urls = Array.from(product_url_set);

console.log(`Parser found ${product_urls.length} unique product URLs`);

return {product_urls};
