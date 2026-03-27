// Extract product name
let product_name = $('[data-isproduct-tracking-disabled]').text_sane();

// Extract brand from product name (first word if it contains "Organic" or similar)
let brand = null;
if (product_name) {
  let words = product_name.split(' ');
  if (words[0] && (words[0].toLowerCase() === 'organic' || /^[A-Z]/.test(words[0]))) {
    brand = words[0];
  }
}

// Extract currency symbol from price text
let price_element = $('.product-comp-v1__price__text span').eq(1);
let price_text = price_element.text_sane();
let currency_symbol = null;
let currency_code = 'USD'; // default

if (price_text) {
  // Extract currency symbol (any non-digit, non-decimal character at start)
  let symbol_match = price_text.match(/^([^\d.,]+)/);
  if (symbol_match) {
    currency_symbol = symbol_match[1].trim();
    // Map common symbols to currency codes
    const currency_map = {
      '$': 'USD',
      '€': 'EUR',
      '£': 'GBP',
      '¥': 'JPY',
      '₹': 'INR',
      'C$': 'CAD',
      'A$': 'AUD'
    };
    currency_code = currency_map[currency_symbol] || 'USD';
  }
}

// Extract price - remove currency symbol and parse
let price = null;
if (price_text) {
  let price_value = parseFloat(price_text.replace(/[^\d.]/g, ''));
  if (!isNaN(price_value)) {
    price = new Money(price_value, currency_code);
  }
}

// Extract price per unit
let price_per_unit_element = $('.color-neutral-80.body-text-xxs span').eq(1);
let price_per_unit_text = price_per_unit_element.text_sane();
let price_per_unit = null;
if (price_per_unit_text) {
  // Extract numeric value from text like "($7.99 / Lb)"
  let match = price_per_unit_text.match(/([\d.]+)/);
  if (match) {
    let price_per_unit_value = parseFloat(match[1]);
    if (!isNaN(price_per_unit_value)) {
      price_per_unit = new Money(price_per_unit_value, currency_code);
    }
  }
}

// Extract quantity from product name
let quantity = null;
if (product_name) {
  let match = product_name.match(/(\d+\s*(?:Lb|Oz|oz|lb|kg|g|ml|L))/i);
  if (match) {
    quantity = match[1];
  }
}

// Extract delivery availability - use eq(0) to get only the first matching element
let availability_delivery = $('.product-details__product-shopping-options table tr[aria-label*="Delivery"] .product-details__product-shopping-options__availability').eq(0).text_sane();

// Extract pickup availability - use eq(0) to get only the first matching element
let availability_pickup = $('.product-details__product-shopping-options table tr[aria-label*="Pickup"] .product-details__product-shopping-options__availability').eq(0).text_sane();

return {
  product_name,
  brand,
  price,
  price_per_unit,
  quantity,
  availability_delivery,
  availability_pickup
};
