const meal = document.querySelector('#meal');
if (meal && !meal.dataset.meal) {
  const hour = new Date().getHours();
  meal.value = hour >= 5 && hour < 11 ? 'breakfast' : hour >= 11 && hour < 16 ? 'lunch' : 'dinner';
}
for (const form of document.querySelectorAll('[data-leftovers-form]')) {
  form.addEventListener('submit', (event) => {
    if (event.submitter?.name) {
      const action = document.createElement('input');
      action.type = 'hidden';
      action.name = event.submitter.name;
      action.value = event.submitter.value;
      action.dataset.submittedAction = '';
      form.append(action);
    }
    for (const button of form.querySelectorAll('button[type="submit"]')) button.disabled = true;
    form.querySelector('[role="status"]').hidden = false;
  });
}
window.addEventListener('pageshow', () => {
  for (const form of document.querySelectorAll('[data-leftovers-form]')) {
    for (const button of form.querySelectorAll('button[type="submit"]')) button.disabled = false;
    for (const action of form.querySelectorAll('[data-submitted-action]')) action.remove();
    form.querySelector('[role="status"]').hidden = true;
  }
});
