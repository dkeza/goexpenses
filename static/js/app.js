(function($) {
	$(document).ready( function() {
		$('#default_accounts_id').change(function() {
			$('#account-selector').submit();
		});

	    setTimeout(function() {
	        $(".alert").alert('close');
	    }, 2000);

    });
})(jQuery);

